---
type: Reference
title: Data-plane access via SDK client
description: Direct mode through the official MIT Go SDK client and the application system database.
status: stable
---

# Data-plane access via SDK client

Relay can observe and control an application through its own system database.
This is **Direct mode**: no Conductor connector, no enterprise package, and no
vendor key. It uses the official MIT Go SDK client, so the application needs no
code change. See [ADR 0015](../adr/0015-direct-mode-system-database.md) for the
decision and [Direct mode](../usage/direct.md) for the operator guide.

Direct mode is useful when no executor is connected, when the application runs
on a post-split SDK, or when the operator wants the database to be the primary
control path. The declarative manifest selects it per application with
`integration: direct`.

## Invariants and safety

Direct database access is governed by strict architectural boundaries:

* **Client first, adapter gated**: Relay communicates with an application's
  system database through the official MIT DBOS Go SDK client
  (`github.com/dbos-inc/dbos-transact-golang/dbos`) by default. Relay currently
  issues no raw SQL against `dbos.*` tables. A version-gated, schema-specific
  adapter is permitted only for an operation the client does not expose, and
  only after a separate ADR defines the schema window, ownership and fencing,
  serialization, signaling, tests, and fail-closed behavior. No adapter exists
  today.
* **Default read-only**: Direct connections default to `read` mode. Lifecycle
  mutations, schedule changes, and recovery are rejected unless the operator
  explicitly opts in to `read-write` mode.
* **Schema safety and migration immunity**: The SDK client constructor
  (`dbos.NewClient`) hardcodes `SkipMigrations: true`, guaranteeing Relay never
  executes migrations against customer application databases. When establishing
  connections, it verifies the schema. A database newer than the pinned client
  expects is tolerated. An older database is refused with an unmigrated-database
  error, leaving the schema untouched.
* **Schema window**: Direct mode v1 pins Go SDK `v1.7.0`, which requires schema
  123 and tolerates newer. The supported window is schema 123 through 125,
  covering the first post-split releases (Go `v1.7.0`, Python `3.3.0`,
  TypeScript `5.3.0`) and the current releases (Go `v1.8.0`, Python `3.4.0`,
  TypeScript `5.4.0`). A schema below 123 is refused; use Attached mode.
  Java `1.2.0` is at schema 114 and uses Attached mode.
* **Supported operations**: workflow queries, steps, aggregates, cancel, resume,
  fork, rewind, delete, schedules, queues, application versions, and recovery.
  Workflow event, notification, and stream listings, export and import, metrics
  that require in-process telemetry, and retention execution stay on the
  executor path and return `503 Service Unavailable` when no executor is
  connected.
* **Integration precedence**: `direct` prefers the database for every operation
  and uses a live executor only for operations the database cannot serve.
  `hybrid` prefers the executor and falls back to the database. `attached`
  ignores the database.
* **Recovery needs a liveness signal**: a dead executor's pending workflows are
  re-enqueued through `ReenqueueForRecovery` after `relay probe` reports the
  executor gone. A deployment-operator signal is planned but has no published
  contract. Without a signal, Direct mode observes and controls but does not
  automatically recover.
* **Auditable responses**: Internal routing tracks source attribution
  (`internal/router.ServedFromTracker`). External emission of `served_from` in
  response headers and metrics is planned for a subsequent observability
  release.

## Configuration

Direct mode is configured per-application in `relay.yaml`:

```yaml
data_plane:
  order-service:
    connection_url: "postgres://dbos_read:secret@db1.internal:5432/order_system_db"
    integration: "direct"
    mode: "read-write"
    statement_timeout_secs: 10
    max_connections: 5

  payment-gateway:
    connection_string_from:
      env: "PAYMENT_DB_URL"
    integration: "hybrid"
    mode: "read"
    statement_timeout_secs: 15
    max_connections: 5
```

Configuration parameters:

* `connection_url`: Database connection string for the application system database. PostgreSQL and SQLite are supported; SQLite-compatible engines are compatible but untested.
* `connection_string_from`: Environment variable (`env`) or secret file (`file`) indirection for database credentials.
* `integration`: `direct`, `hybrid`, or `attached`. `hybrid` is the default.
* `mode`: Access mode (`read` or `read-write`). Defaults to `read`.
* `statement_timeout_secs`: Enforced statement timeout bounding queries.
* `max_connections`: Maximum pool size allocated for this application's client.

## Multi-database fleet architecture

In a DBOS Transact deployment, each registered application operates with its own isolated system database:

* **Default executor model**: Each executor service manages its own database connection locally via the standard DBOS configuration (`DBOS_SYSTEM_DATABASE_URL` or `dbos-config.yaml`). When executors register with Relay over WebSocket, they handle workflow execution, step recording, and transaction persistence directly against their respective application databases. Relay acts strictly as the control plane coordinator, dispatching commands without holding or inspecting application database credentials.
* **Direct model**: When an operator configures a connection in `relay.yaml`, Relay maintains independent client pools for each configured application. Requests for `order-service` route to the `order-service` database pool, while requests for `payment-gateway` route to the `payment-gateway` pool. Relay completely isolates connection pools and schemas across applications. In `direct` integration, the database is the preferred path; in `hybrid`, it is the fallback; in `attached`, it is unused.

## Design rationale: why database connections are not managed in the UI

Relay deliberately does not manage, configure, or expose database credentials in the web dashboard interface:

1. **Control plane and data plane separation**: The control plane coordinates execution workflows, dispatches tasks, and collects operational telemetry. Application database storage belongs to the data plane. Storing application database secrets in the control plane violates this separation of concerns.
2. **Credential security and isolation**: Database connection strings contain sensitive credentials such as passwords, private VPC endpoints, and authentication certificates. Submitting credentials through web forms or exposing them in dashboard REST payloads exposes them to browser history, client-side memory, and proxy logs. In Relay, database connection strings are supplied exclusively through server-side environment variables, secret files, or infrastructure-as-code manifests (`relay.yaml`).
3. **Upstream DBOS Conductor protocol parity**: The upstream DBOS Conductor REST API specification (`/v2/orgs/{org}/apps`) does not accept, store, or return database credentials. Adding database configuration endpoints to Relay would diverge from the upstream contract and create non-standard behavior for DBOS SDK clients.
4. **Direct status without credential exposure**: Rather than displaying sensitive connection strings, Relay reports connection readiness through the application's integration mode and executor health in the dashboard. This lets operators verify availability across applications without leaking infrastructure secrets.

## Wire and Direct mode parity

Administrative operations behave identically whether dispatched over WebSocket to a connected
executor or applied directly through the SDK client in Direct mode.

### Cancellation semantics

WebSocket cancellation requests and Direct mode cancellation calls apply the exact same database update. The abridged SQL below illustrates the status transition (source: `https://github.com/dbos-inc/dbos-transact-golang` v1.7.0 commit `339705771d7e198c5bac9e213903820c0575e6c1`, `dbos/internal/sysdb/system_database.go` `CancelWorkflows`):

```sql
-- Abridged illustration (source: dbos/internal/sysdb/system_database.go, v1.7.0)
UPDATE dbos.workflow_status
SET status = 'CANCELLED'
WHERE workflow_uuid = $1
  AND status NOT IN ('SUCCESS', 'ERROR', 'CANCELLED');
```

Neither protocol path attempts to asynchronously interrupt running operating system threads,
goroutines, or asyncio tasks in an active executor. All three official SDKs (Go, Python, TypeScript)
detect cancellation at step boundaries:

1. The executor executes workflow step functions locally.
2. At each step completion boundary, the executor calls `UpdateWorkflowOutcome` against the system database.
3. Because the status column is no longer `PENDING`, the conditional update matches zero rows.
4. The executor detects `rowsAffected == 0`, immediately ceases further step execution, and marks its local workflow execution context as cancelled.

Consequently, cancellations executed in Direct mode observe the identical execution boundary
and safety guarantees as live WebSocket cancellations.

### Resume semantics

WebSocket resume requests and Direct mode resume calls transition workflows back into the queue table. The abridged SQL below illustrates the transition (source: `https://github.com/dbos-inc/dbos-transact-golang` v1.7.0 commit `339705771d7e198c5bac9e213903820c0575e6c1`, `dbos/internal/sysdb/system_database.go` `ResumeWorkflows`):

```sql
-- Abridged illustration (source: dbos/internal/sysdb/system_database.go, v1.7.0)
UPDATE dbos.workflow_status
SET status = 'ENQUEUED',
    queue_name = '_dbos_internal_queue',
    recovery_attempts = 0,
    started_at_epoch_ms = NULL,
    completed_at = NULL
WHERE workflow_uuid = $1
  AND status NOT IN ('SUCCESS', 'ERROR');
```

Any live executor polling `_dbos_internal_queue` via `DequeueWorkflows` picks up the resumed workflow
and executes remaining steps.

### Fork semantics

WebSocket fork requests (`fork_workflow`) and Direct mode fork mutations apply identical workflow initialization semantics. The Direct client invokes `client.ForkWorkflow` (`internal/dataplane/client.go`), creating a new workflow record with state copied up to the requested step index and enqueued for execution on `_dbos_internal_queue`.

### Fork filtering

The `is_fork` filter is applied by the executor on the wire and by the Go client in Direct mode. Upstream added the filter in Go SDK v1.6.0 (source: `https://github.com/dbos-inc/dbos-transact-golang` commit `de29e534d9bd788e85f77dd3dce63b20571b8f1b`) and in Java 1.2.0 (source: `https://github.com/dbos-inc/dbos-transact-java` commit `0f4fe187b20bd6f5f735d8f99d810aeb90b8314d`). The Direct client pins Go SDK v1.7.0, which supports `WithFilterIsFork`, so the filter is applied consistently on both paths.

### Durable sleep and message receive during cancellation

When a running workflow is cancelled in Direct mode while suspended in a durable sleep (`sleep`)
or waiting on an event message (`recv`):

* **Go SDK** (`dbos/workflow.go` `Sleep`): The worker process sleeps for the remaining
  duration via an in-memory timer. On timer expiration, the sleep function returns without checking the
  system database. Cancellation is detected only at the next checkpointed step boundary or when attempting
  to finalize the workflow outcome (`UpdateWorkflowOutcome`).
* **Python SDK** (`dbos/_dbos.py` `sleep`, `dbos/_sys_db.py` `recv`): The worker thread sleeps
  until the local duration completes (`time.sleep` or `asyncio.sleep`). Recheck intervals in `recv`
  poll exclusively for incoming notifications on `dbos.notifications`. On wakeup, the workflow continues
  and detects cancellation at the subsequent step boundary.
* **TypeScript SDK** (`src/system_database.ts` `durableSleepms`, `recv`): The worker sleeps locally until the
  target timestamp. However, immediately upon timer expiration in `durableSleepms` and upon event resolution
  in `recv`, the SDK explicitly invokes `checkIfCanceled(workflowID)`. If the row was updated to `CANCELLED`
  in `dbos.workflow_status` during the sleep window, the executor immediately throws `DBOSWorkflowCancelledError`
  and aborts execution before running any subsequent user steps.

### Served-from auditing

Internal routing tracks request dispatch attribution (`internal/router.ServedFromTracker`)
between live WebSocket executors and Direct mode reads. Surfacing `served_from`
in external HTTP response headers (`X-Relay-Served-From`) and Prometheus metrics
(`relay_requests_served_total`) is planned for the subsequent Scale and Observability tier.

## Operational metrics and alerting

Application-level Conductor alerting rules are strictly constrained to the standardized OpenAPI enum (`WorkflowFailure`, `SlowQueue`, `UnresponsiveApplication`) per ADR 0008.

Independent of application-level alerting rules, Relay records the source that fulfilled each request in the `relay_requests_served_total` metric with the `served_from` label (`executor` or `database`). In the initial release, the metrics endpoint emits `dbos_conductor_v1_executor_count`; fallback ratio alert rules (such as `RelayHighDatabaseFallbackRatio`) are designed for the observability tier and will activate when the corresponding counter series is enabled:

```yaml
# deploy/observability/prometheus/relay-alerts.yaml (planned rule)
- alert: RelayHighDatabaseFallbackRatio
  expr: sum(rate(relay_requests_served_total{served_from="database"}[5m])) / sum(rate(relay_requests_served_total[5m])) > 0.5
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High data-plane database fallback ratio"
    description: "More than 50% of requests are served via database fallback due to missing or unresponsive executors."
```

An elevated fallback ratio indicates that executors are crash-looping, failing to register,
or unable to keep up with incoming request volumes.
