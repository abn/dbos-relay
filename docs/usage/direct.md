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
  lifecycle mutations, schedule changes, or recovery.
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

Direct mode serves workflow queries, steps, aggregates, cancel, resume, fork,
rewind, delete, schedules, queues, application versions, and recovery. Every
operation uses the official Go client; Relay does not issue raw SQL against
`dbos.*` tables.

The following operations stay on the executor path and return
`503 Service Unavailable` when no executor is connected:

- workflow event, notification, and stream listings;
- export and import;
- metrics that require in-process telemetry;
- retention execution.

Payloads are read in their stored format, including DBOS portable JSON. Python
and TypeScript default to native serializers; configure the portable JSON
serializer when payloads need to be readable across languages. In Python use
`DBOSPortableJSONSerializer`; in TypeScript use a serializer whose `name()` is
`portable_json`. The cross-language fixtures in `tests/direct` are a worked
example. Values Relay cannot decode are displayed as opaque and are not written
back. Enqueue, send, and set-event are not exposed by this release.

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
