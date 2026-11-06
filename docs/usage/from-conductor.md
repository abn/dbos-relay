---
type: Guide
title: Migrating from Conductor
description: Move a DBOS Transact application from Conductor to Relay without changing application code.
status: stable
---

# Migrating from Conductor

Relay controls a DBOS Transact application through its existing system database
and, where the SDK still provides it, the Conductor executor protocol. Moving
from Conductor does not copy workflow state. The application keeps its own
database and its code; you change the control plane it reports to, or connect
Relay to the database directly.

Direct mode is stable for schema 123 through 125. Attached mode remains
supported within its SDK window. This guide covers both cutover paths. Read
[Supported SDKs](supported-sdks.md) and [Direct mode](direct.md) before you
start.

## Before you start

- Take a snapshot of the application's system database and of Relay's own
  control-plane database if Relay is already deployed.
- Keep the existing control plane reachable until the verification checks pass
  where one exists. An Attached cutover should be recoverable by changing the
  application configuration back. A post-split Direct cutover may have no
  Conductor connector to return to, so plan that gap before switching.
- Use a maintenance window for the application. One control plane should own a
  given application at a time; do not leave Conductor and Relay both issuing
  recovery or mutations for the same application.
- Note that Relay's control-plane metadata is separate from Conductor's. API
  keys, roles, alert rules, and audit history are not imported. Recreate what
  the deployment needs.

## Choose the mode

The SDK version and the application's system database schema decide the path.

| Application | Mode | What changes |
| --- | --- | --- |
| Go `v1.6.0` and below | Attached | Conductor URL and key |
| Python `3.2.0` and below | Attached | Conductor URL and key |
| TypeScript `5.2.11` and below | Attached | Conductor URL and key |
| Java `1.2.0` | Attached | Conductor URL and key |
| Go `v1.7.0` or `v1.8.0`, schema 123 through 125 | Direct | Relay `data_plane` connection; probe for automatic recovery |
| Python `3.3.0` or `3.4.0`, schema 123 through 125 | Direct | Relay `data_plane` connection; probe for automatic recovery |
| TypeScript `5.3.0` or `5.4.0`, schema 123 through 125 | Direct | Relay `data_plane` connection; probe for automatic recovery |

A post-split SDK release is not supported in Attached mode. The public SDK no
longer contains the connector, and Relay does not test a proprietary connector.
A schema below 123 is not supported in Direct mode; use Attached mode if the
SDK still contains the public connector. No post-split release below the schema
floor exists today.

Java `1.2.0` uses Attached mode. Attached recovery is verified against schema
114: after the active executor is killed, Relay marks it dead and a surviving
executor recovers the workflow exactly once. Use the Attached cutover checks
before moving a Java service.

## Attached cutover

Use this path for an application whose SDK still contains the public connector.

1. Create the Relay organisation and an API key for the application, if the
   deployment requires authentication.
2. Point the application's Conductor settings at Relay:

   ```bash
   export DBOS__CLOUD=true
   export DBOS__CONDUCTOR_APP_NAME=checkout
   export DBOS__CONDUCTOR_URL=wss://relay.example.com
   export DBOS__CONDUCTOR_KEY=...
   ```

3. Restart the application. Relay should list the application and its executor
   as healthy.
4. Open a workflow in the Relay console and verify its steps and output.
5. Test one safe operation in a staging environment, such as cancelling a test
   workflow, before relying on mutations in production.

Relay does not require an application code change for a supported Attached
window. The application still uses the connector and executor that ship with
its SDK.

## Direct cutover

Use this path for a post-split SDK at schema 123 through 125. Direct mode uses
the application's system database through the official MIT Go client. It does
not use the connector and does not store the database URL in Relay.

1. Add the application to `relay.yaml` and configure its database connection:

   ```yaml
   version: "1"
   organisation: "production"

   applications:
     - name: "checkout"

   data_plane:
     checkout:
       connection_string_from:
         env: "CHECKOUT_DB_URL"
       integration: "direct"
       mode: "read"
       statement_timeout_secs: 10
       max_connections: 5
   ```

   The `applications` name and the `data_plane` key must match the DBOS
   `app_name`. Put the credential in an environment variable or secret file,
   never in the committed manifest.

2. Provision control-plane records, then start or restart Relay with the
   manifest:

   ```bash
   relay apply -f relay.yaml --database-url "$RELAY_DATABASE_URL"
   export RELAY_CONFIG=/etc/relay/relay.yaml
   relay serve
   ```

   `relay apply` does not register data-plane connections. `relay serve` applies
   the manifest at startup and registers each non-Attached entry; schema
   verification happens when the client is created. If initialization fails,
   check the server log for `failed eager data-plane client initialization` or
   `failed lazy data-plane client initialization`; Relay keeps the application
   configured and retries after a short backoff. The client refuses a database
   below the supported window. Relay does not run migrations or arbitrary SQL
   against the application database. Restart the server after editing the
   manifest.

3. Keep `mode: read` for the first pass. Verify the application, workflows,
   steps, queues, and schedules in the console.
4. Switch to `mode: read-write` only when the operator needs lifecycle
   mutations, schedule changes, or recovery. Read-only Direct mode fails closed;
   a rejected mutation is not retried over an executor socket. Edit the manifest
   and restart Relay so the data plane is registered with the new mode.
5. For automatic recovery, set `RELAY_INTERNAL_SECRET` on Relay and run
   `relay probe` beside the application with the same executor identity the SDK
   uses. The probe must share the application process lifetime. An
   operator-provided liveness signal is planned but has no published contract
   yet; do not plan on it. Without a signal, Direct mode observes and controls
   but does not detect a dead executor.

   ```bash
   export DBOS_APP_NAME="checkout"
   export DBOS__VMID="checkout-0"
   export DBOS__APPVERSION="v1.2.0"
   export RELAY_INTERNAL_SECRET="..."

   relay probe --relay-url "https://relay.example.com" \
     --org production --app "$DBOS_APP_NAME"
   ```

6. Test one safe mutation and one recovery scenario in staging before the
   production cutover.

Direct mode keeps some operations on the executor path. Workflow event,
notification, and stream listings, export, and import return `503 Service
Unavailable` when no executor is connected. A single workflow event can be read
by key through the Relay event lookup. Enqueue and send are Relay extensions,
and send is the operator path for unblocking a workflow that waits on Recv. Set
event stays application-owned because the MIT Go client exposes it only to a
running workflow, so a workflow that already depends on set-event needs an
application change to Send and Recv. Metrics and retention are served from the
system database. Payloads are read in their stored format; applications that
need cross-language payloads should use portable JSON. The full list is in
[Direct mode](direct.md).

## What moves and what does not

- **Workflow and schedule state** stays in the application's system database.
  Relay reads and, in read-write mode, controls that state. There is no bulk
  copy of workflow history.
- **Relay's control-plane metadata** stays in Relay's own database. Conductor's
  API keys, roles, alert rules, audit history, and other control-plane records
  are not imported.
- **Application configuration** stays with the application. Attached mode
  changes the Conductor URL and key; Direct mode adds a Relay data-plane
  connection and, where automatic recovery is required, a liveness signal.
- **API clients** should be pointed at the Relay API URL with a Relay
  credential. Compatible operations are listed in the
  [compatibility tiers](../design/compatibility-tiers.md) and the
  [REST surface inventory](../discovery/rest-surface.md).

## Running Relay

For a production cutover, plan the control plane as its own service:

- Relay plus a PostgreSQL control-plane database, with the database backed up
  separately from the application system databases. Embedded SQLite is
  available for a single-node pilot, not as the HA reference.
- More than one Relay instance behind a TLS reverse proxy when the control plane
  must survive a node loss. See [Operations and migrations](operations.md).
- A probe beside each Direct application where automatic recovery is required.
  The operator signal is not yet a supported path.
- Monitoring and on-call for Relay, its database, the probes, and the
  application databases. A control-plane outage removes fleet control and
  recovery even though application execution continues.
- A tested restore of the Relay database and a tested application-database
  restore before the cutover.

This guide does not estimate infrastructure or team cost. Size the deployment
against the number of applications, executors, retained workflows, and the
recovery target you require.

## Common questions

**How do I check the application schema?**

Relay does not expose a schema-inspection command. Start Relay in read-only
Direct mode against a staging copy and check the server log. The Go client
verifies the schema when it initializes; a failure appears as
`failed eager data-plane client initialization` and is retried with
`failed lazy data-plane client initialization`. Schema 123 through 125 is
supported; 123 and 125 are tested, and 124 is covered by the client's migration
tolerance. A schema below 123 is refused.

**Can I run a mixed fleet?**

Yes. The mode is chosen per application in `relay.yaml`, so Attached and Direct
applications can share one Relay instance. Use one control plane per application
during the cutoff. `hybrid` is also available as a per-application transition
mode: it prefers a live executor and falls back to the database, and still needs
a `data_plane` entry.

**Which operations need a live executor?**

| Mode | Behaviour |
| --- | --- |
| Attached | All operations go through the executor connection. |
| Hybrid | Executor first; database fallback for operations the database supports. |
| Direct | Database for workflow queries, steps, aggregates, cancel, resume, fork, rewind, delete, schedules, queues, application versions, recovery, enqueue, send, event lookup by key, metrics, and retention. Events, notifications, and streams listings, export, and import need an executor and return `503` without one. |

**Is there a Kubernetes operator?**

Not yet as a documented contract. The supported liveness path is `relay probe`.
An operator signal is planned but has no published endpoint or lifecycle
contract; do not design a migration around it.

**Does Java recovery work?**

Attached recovery for Java `1.2.0` at schema 114 is verified. Relay dispatches
recovery to a surviving executor after the killed executor is marked dead, and
the workflow completes exactly once. Other Java operations remain in Attached
mode.

**What about enqueue, send, and set-event?**

Enqueue and send are Relay extension endpoints. They use the Direct data plane
and DBOS portable JSON. Set-event remains application-owned: the MIT Go client
exposes it only to a running workflow. For new workflows that may need an
operator to unblock them, use Send and Recv. A workflow that already depends on
set-event needs an application change; Relay does not write keyed events.

**How do schedules move?**

Schedule state is in the application system database. Direct mode manages
schedule rows through the Go client, and the SDK scheduler runs in the
application process. Relay does not provide an automated cross-control-plane
schedule handoff or a no-double-fire guarantee for a cutover. Use a maintenance
window that excludes scheduled firing, use one control plane for the
application, confirm that duplicate schedule rows are not present, and test the
expected schedules before and after the cutover.

**How do alert rules move?**

They do not move automatically. Relay does not import Conductor alert rules and
does not provide a rule converter or a mapping promise. Recreate the rules you
need with [Declarative operations](declarative.md). The rule surface follows the
OpenAPI schema; this is not a claim that existing Conductor rules are migrated
one-to-one. Deliver one test alert to the on-call destination before the
cutover.

**Do I need to change payload serializers?**

Not for core operation. Payloads are read in their stored format. If workflows
exchange payloads across languages, use portable JSON and plan the change before
the cutover; values Relay cannot decode are displayed as opaque. Do not change
serializers under in-flight workflows without a staging test.

**Which application databases are supported?**

PostgreSQL and SQLite are supported and tested. SQLite-compatible engines such
as Turso use the client's SQLite driver and are compatible, but Relay does not
test them; verify the target engine before production use. The schema window is
the same for all three.

**How do I restore control if Relay fails on a post-split SDK?**

Relay cannot re-enable the proprietary enterprise connector. Restore the prior
control plane and its configuration if you have one, or accept a control gap
while the application continues durable execution. Test that path before the
cutover. Relay can be restarted, but it cannot hand control back to a connector
it does not manage.

**Which connection field wins?**

If `connection_string_from` is set, it takes precedence and reads from the named
environment variable or secret file. Otherwise `connection_url` is used with
environment expansion. Use one form consistently.

**Does `relay apply` register a data-plane connection?**

No. `relay apply` provisions control-plane records. `relay serve` with
`RELAY_CONFIG` registers data-plane connections at startup and verifies the
schema. Restart the server after changing a mode or connection.

**What support is included?**

Relay is community-supported, best effort, with no SLA. Use the public
repository for questions and fixes. The migration guide does not imply a
support contract or a guaranteed response time.

**Is Relay in the application execution path?**

No. The DBOS runtime in the application continues to execute and persist
workflows in its own database. A Relay outage removes the fleet view, control
operations, and cross-node recovery; it does not stop durable execution already
running in the application.

## Rollback

For Attached mode, stop the application, restore the previous Conductor URL and
key, and restart it. The application's workflow state is unchanged.

For Direct mode, disabling the data-plane entry stops Relay from reading the
application database after you apply the change and restart Relay. Any workflow
mutations or recovery Relay already performed remain in the application
database; they are not undone by removing the integration. Relay cannot manage
or re-enable the proprietary enterprise connector. For a post-split SDK, restore
your prior control plane and its configuration, or accept a control gap while
the application keeps executing. Test that rollback before the cutover.

Relay database upgrades are covered separately in
[Operations and migrations](operations.md). A Relay binary cannot run against a
newer Relay schema, so use the migration and rollback procedures there rather
than treating application rollback and Relay rollback as one operation.

## Cutover checklist

- [ ] Application system database snapshot taken.
- [ ] Relay control-plane database snapshot taken, if Relay already exists.
- [ ] Mode selected from the SDK and schema table.
- [ ] Relay organisation and API key created where required.
- [ ] Database credential stored outside the manifest, if Direct mode.
- [ ] Old control plane stopped for the application during cutover.
- [ ] Application connected, or the data-plane client initialized without a failure log.
- [ ] Executor healthy, or the Direct data-plane client initialized.
- [ ] Workflow read and DAG inspected.
- [ ] One safe mutation tested in staging.
- [ ] Recovery signal tested where automatic recovery is required.
- [ ] Alert rules, roles, and audit expectations recreated or accepted as absent.
- [ ] Rollback path recorded.
