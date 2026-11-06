---
type: Guide
title: Direct mode
description: Run Relay against the application system database with no Conductor connector.
status: stable
---

# Direct mode

Direct mode lets Relay observe and control a DBOS Transact application through
the application's own system database. It uses the official MIT Go client, so
it works with SDK releases that no longer ship the Conductor connector. The
application needs no code change and no enterprise package.

Direct mode is one of three integration modes. `hybrid` keeps the executor
first and falls back to the database. `attached` uses the executor only and
ignores the database. The mode is configured per application in the declarative
manifest.

## Requirements

- A supported system database schema. Direct mode v1 covers schema 123 through
  125, which includes the first post-split releases (Go `v1.7.0`, Python
  `3.3.0`, TypeScript `5.3.0`) and the current releases (Go `v1.8.0`, Python
  `3.4.0`, TypeScript `5.4.0`). A schema below 123 is refused; use Attached
  mode instead.
- A database URL for the application system database, supplied through an
  environment variable or a secret file. Relay never stores the URL in its own
  database.
- The manifest application name must match the DBOS application name
  (`app_name`). Relay scopes the client to that name, so a mismatch hides the
  application's rows from Direct mode.
- Read-only access by default. Enable read-write only when the operator needs
  lifecycle mutations, schedule changes, enqueue, send, retention execution, or
  recovery.
- For automatic recovery, a liveness signal from `relay probe` or a deployment
  operator. Without it, Direct mode observes and controls but does not detect a
  dead executor.

## Configuration

```yaml
version: "1"
organisation: "production"

applications:
  - name: "order-service"

data_plane:
  order-service:
    connection_string_from:
      env: "ORDER_SERVICE_DB_URL"
    integration: "direct"
    mode: "read-write"
    statement_timeout_secs: 10
    max_connections: 5
```

Apply the manifest with `relay apply -f relay.yaml`. Relay registers the
connection and routes requests for `order-service` to the database first. A
live executor is used for operations the database path does not support.

## Liveness probe

`relay probe` is a single static binary. Run it beside the application process,
sharing the process lifetime, and pass the same executor identity the SDK uses.
It sends a heartbeat every ten seconds. A missed heartbeat expires after the
configured TTL and starts the executor grace period, after which Relay
re-enqueues the dead executor's pending workflows through the system database.
The probe endpoint requires `RELAY_INTERNAL_SECRET`; Relay does not mount it
when the secret is unset.

```bash
export DBOS_APP_NAME="order-service"
export DBOS__VMID="order-service-0"
export DBOS__APPVERSION="v1.2.0"
export RELAY_INTERNAL_SECRET="..."

relay probe --relay-url "http://relay.internal:8090" --org production --app "$DBOS_APP_NAME"
```

On graceful shutdown the probe sends a stop heartbeat. If the process is
killed, the heartbeat expires and Relay starts the grace period.

In Kubernetes, a deployment operator can supply the same signal from pod
lifecycle events instead of running the probe. Both paths feed the same
recovery dispatcher.

## Supported operations

Direct mode serves the following operations from the application system
database. Relay extension endpoints add enqueue and send with DBOS portable
JSON, plus keyed event lookup; they are not part of the vendored Conductor
OpenAPI.

| Operation | Database path |
| --- | --- |
| Workflow queries, steps, aggregates | Official client reads |
| Cancel, resume, fork, rewind, delete | Official client mutations |
| Schedules, queues, application versions, recovery | Official client operations |
| Enqueue and send | Official client writes with DBOS portable JSON |
| Workflow event lookup by key | Official client reads by key |
| Metrics | Workflow and step counts grouped by name over the requested window |
| Retention execution | Deletes terminal workflow history in batches and cancels work that outlived the global timeout |

Metrics use `workflow_status` creation times for workflow counts and
`operation_outputs` completion times for step counts, matching the public Python
and TypeScript SDK implementations. They report counts, not live CPU, memory, or
in-flight executor telemetry; use a probe, OpenTelemetry, or Attached mode for
that.

Retention follows the documented DBOS policy. It deletes only `SUCCESS`,
`ERROR`, `CANCELLED`, and `MAX_RECOVERY_ATTEMPTS_EXCEEDED` workflows, leaves
running, enqueued, and delayed work alone, and resolves the more restrictive of
the time and rows thresholds. The rows threshold counts the configured
application's workflows because Direct mode scopes the client to that
application; a system database shared by several applications needs the same
policy on each entry. Retention requires `mode: read-write`.

The following operations have no database path because the MIT Go client has no
operation that can serve them:

| Operation | Direct mode |
| --- | --- |
| Workflow event, notification, and stream listings | Executor only. The client reads events and streams by key, not by listing, and Relay does not read the system tables directly. A single event can be read by key through the Relay extension below. |
| Export | Executor only. The serialized workflow format belongs to the executor and is SDK-specific. |
| Import | Not supported. The Go client has no import operation, and Direct mode does not retry a mutation over an executor socket. |

Event, notification, and stream listings and export fall through to a live
executor in Direct mode and return `503 Service Unavailable` with the reason
when none is connected. Import fails closed in Direct mode for the same reason
a rejected mutation does: retrying it over a socket could apply it twice. Use
Attached or Hybrid mode for an SDK that still ships the connector and needs
import.

Every operation uses the official Go client; Relay does not issue raw SQL
against `dbos.*` tables.

Payloads are read in their stored format, including DBOS portable JSON. Python
and TypeScript default to native serializers; configure the portable JSON
serializer when payloads need to be readable across languages. In Python use
`DBOSPortableJSONSerializer`; in TypeScript use a serializer whose `name()` is
`portable_json`. The cross-language fixtures in `tests/direct` are a worked
example. Values Relay cannot decode are displayed as opaque and are not written
back.

## Relay extension endpoints

Enqueue and send are Relay extensions. The vendored Conductor OpenAPI defines no
operation for either, so Relay adds two routes of its own rather than presenting
them as Conductor contract operations. They use the same authentication,
application scoping, read-write gate, and problem-details errors as the rest of
the API.

`POST /v2/orgs/{orgName}/apps/{appName}/workflows/enqueue` writes a workflow
row to the application system database and returns `201 Created` with the new
workflow ID and a `Location` header:

```json
{
  "workflowName": "orders",
  "queueName": "orders-q",
  "payload": {"order": 42},
  "workflowId": "optional-caller-supplied-id",
  "appVersion": "optional-version",
  "queuePartitionKey": "optional-partition"
}
```

```json
{"workflowId": "5c6f0d9e-..."}
```

`POST /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/send` writes a
message row for the destination workflow and returns `204 No Content`:

```json
{
  "topic": "orders",
  "payload": {"status": "ready"},
  "idempotencyKey": "optional-key"
}
```

The payload is any JSON value. Relay stores it with the DBOS portable JSON
serializer, so an executor in another language can decode it. A payload that is
missing or not valid JSON is rejected with `400 Bad Request`; Relay does not
re-encode an opaque value.

`GET /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/event?key={key}`
reads one event value for the target workflow through the official client's
keyed event operation:

```json
{"key": "orders", "value": {"status": "ready"}}
```

`value` is the decoded value stored under the key. Portable JSON is readable
across languages. A key Relay cannot find returns `404 Not Found`, and an empty
or missing `key` returns `400 Bad Request`. A stored value in a serializer the
client cannot decode returns `503 Service Unavailable`. The lookup is a read,
so a read-only Direct connection serves it.

The routes follow the application integration mode. Direct and Hybrid use the
database. Attached refuses with `501 Not Implemented`. A read-only Direct
connection fails closed for enqueue and send with `403 Forbidden`, and Relay
does not retry the write over an executor socket. A missing data-plane
connection also returns `501`.

Set event is application-owned. The MIT Go client exposes `SetEvent` only to a
running workflow, and the exported `dbos/internals` seam does not export the
input type its `SystemDatabase.SetEvent` requires. Relay does not reflect into
the client or write the event table directly, so there is no operator set-event
action. A workflow that needs an operator to unblock it should use Send and
Recv: the operator sends a message from the console or the send endpoint, and
the workflow waits on `Recv` for that topic. This is the recommended pattern for
new workflows because it works in Direct mode and has a documented operator
path. An existing workflow that already depends on set-event needs an
application change to that pattern before Direct mode can unblock it.

## Security

Direct mode needs database credentials. Treat them as operator secrets:

- pass them through `connection_string_from` or `${ENV_VAR}` expansion, never
  in the committed manifest;
- grant a least-privilege role: read-only for observation, a separate
  read-write role only where mutations are required;
- restrict the Relay host's network path to the database;
- keep `RELAY_INTERNAL_SECRET` set and protect the probe endpoint.

Relay does not run migrations, DDL, or arbitrary queries against the
application database. The SDK client verifies the schema and refuses an
unknown generation. A read-only connection fails closed; Relay does not retry a
mutation over an executor socket to bypass it. The probe endpoint is accepted
only for applications configured with `integration: direct`.

## When to use Attached mode instead

Use Attached mode for applications on SDK releases that still contain the
public connector, for Java (schema 114 in release `1.2.0`), and when live
executor telemetry is required. The two modes can run side by side; the
manifest chooses per application.
