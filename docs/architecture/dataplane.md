---
type: Reference
title: Data-plane access via SDK client
description: Optional application database access via official Go SDK client.
status: draft
---

# Data-plane access via SDK client

Relay provides an optional, opt-in data plane for applications that configure direct database access.
This allows Relay to serve workflow queries and administrative operations even when zero executors
are actively connected.

## Invariants and safety

Direct database access is governed by strict architectural boundaries:

* **Official SDK client only**: Relay communicates with an application's system database exclusively
  through the official DBOS Go SDK client library (`github.com/dbos-inc/dbos-transact-golang/dbos`). Relay never issues raw SQL queries (`SELECT * FROM dbos.*`).
* **Default read-only**: Data plane connections default to `read` mode. Mutation operations (`CancelWorkflow`,
  `ResumeWorkflow`, `ForkWorkflow`) are rejected unless the operator explicitly opts in to `read-write` mode.
* **Schema safety and migration immunity**: The SDK client constructor (`dbos.NewClient`) hardcodes `SkipMigrations: true`, guaranteeing Relay never executes migrations against customer application databases. When establishing connections, it calls `VerifyMigrations`. If the target database schema version is newer than Relay's Go SDK version expects, it operates safely without error. If the schema version is older, it fails fast with `*models.UnmigratedDatabaseError` and refuses connection, leaving the schema untouched.
* **Multi-SDK schema compatibility**: Direct data-plane database access requires a system database schema version of v107 or higher, as expected by the official Go client library. DBOS Transact applications written in Go, Python, and TypeScript share compatible schema versions in current releases. Applications using the upstream Java SDK (such as version 0.8.0, which provisions system database schema v19) cannot be read via the Go SDK data-plane client and fail migration verification safely. However, Java applications remain fully functional with Relay through the live WebSocket protocol and standard REST control-plane operations.
* **Deferred operations and serialization boundary**: Inter-workflow communication and event retrieval (`GetEvent`, `Send`, `Enqueue`, `ReadStream`) are deferred in v1 (returning HTTP 501 Not Implemented or HTTP 503) due to cross-language serialization boundaries between Go, Python, TypeScript, and Java runtimes.
* **Executor precedence**: When a live executor is connected, requests are routed to the executor over
  WebSocket. The data plane acts as a fallback when no executors are reachable.
* **Auditable responses**: Internal routing tracks source attribution (`internal/router.ServedFromTracker`).
  External emission of `served_from` in response headers and metrics is planned for a subsequent observability release.

## Configuration

Data-plane access is configured per-application in `relay.yaml`:

```yaml
data_plane:
  order-service:
    connection_url: "postgres://dbos_read:secret@db1.internal:5432/order_system_db"
    mode: "read"
    statement_timeout_secs: 10
    max_connections: 5

  payment-gateway:
    connection_string_from:
      env: "PAYMENT_DB_URL"
    mode: "read"
    statement_timeout_secs: 15
    max_connections: 5
```

Configuration parameters:

* `connection_url`: PostgreSQL connection string for the application system database.
* `connection_string_from`: Environment variable (`env`) or secret file (`file`) indirection for database credentials.
* `mode`: Access mode (`read` or `read-write`). Defaults to `read`.
* `statement_timeout_secs`: Enforced statement timeout bounding queries.
* `max_connections`: Maximum pool size allocated for this application's data plane client.

## Multi-database fleet architecture

In a DBOS Transact deployment, each registered application operates with its own isolated PostgreSQL database:

* **Default executor model**: Each executor service manages its own database connection locally via the standard DBOS configuration (`DBOS_SYSTEM_DATABASE_URL` or `dbos-config.yaml`). When executors register with Relay over WebSocket, they handle workflow execution, step recording, and transaction persistence directly against their respective application databases. Relay acts strictly as the control plane coordinator, dispatching commands without holding or inspecting application database credentials.
* **Direct fallback model**: If an operator configures data-plane fallback in `relay.yaml`, Relay maintains independent client pools for each configured application. Requests for `order-service` route to the `order-service` database pool, while requests for `payment-gateway` route to the `payment-gateway` pool. Relay completely isolates connection pools and schemas across applications.

## Design rationale: why database connections are not managed in the UI

Relay deliberately does not manage, configure, or expose database credentials in the web dashboard interface:

1. **Control plane and data plane separation**: The control plane coordinates execution workflows, dispatches tasks, and collects operational telemetry. Application database storage belongs to the data plane. Storing application database secrets in the control plane violates this separation of concerns.
2. **Credential security and isolation**: Database connection strings contain sensitive credentials such as passwords, private VPC endpoints, and authentication certificates. Submitting credentials through web forms or exposing them in dashboard REST payloads exposes them to browser history, client-side memory, and proxy logs. In Relay, database connection strings are supplied exclusively through server-side environment variables, secret files, or infrastructure-as-code manifests (`relay.yaml`).
3. **Upstream DBOS Conductor protocol parity**: The upstream DBOS Conductor REST API specification (`/v2/orgs/{org}/apps`) does not accept, store, or return database credentials. Adding database configuration endpoints to Relay would diverge from the upstream contract and create non-standard behavior for DBOS SDK clients.
4. **Data plane status without credential exposure**: Rather than displaying sensitive connection strings, Relay reflects data plane operational readiness through live executor health indicators in the dashboard (`Executor Hub (N active)` versus `No Live Executors`). This allows operators to verify data-plane availability across applications at a glance without leaking infrastructure secrets.

## Wire and data-plane parity

Administrative operations behave identically whether dispatched over WebSocket to a connected
executor or applied directly through the SDK client data plane.

### Cancellation semantics

WebSocket cancellation requests and data-plane cancellation calls apply the exact same database update. The abridged SQL below illustrates the status transition (source: `https://github.com/dbos-inc/dbos-transact-go` commit `ab56911fdd78552e1e7fe648cff7c831a1e760c8`, `dbos/internal/sysdb/system_database.go` `CancelWorkflows` lines 2005-2071):

```sql
-- Abridged illustration (source: dbos/internal/sysdb/system_database.go lines 2005-2071, commit ab56911)
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

Consequently, cancellations executed via data-plane fallback observe the identical execution boundary
and safety guarantees as live WebSocket cancellations.

### Resume semantics

WebSocket resume requests and data-plane resume calls transition workflows back into the queue table. The abridged SQL below illustrates the transition (source: `https://github.com/dbos-inc/dbos-transact-go` commit `ab56911fdd78552e1e7fe648cff7c831a1e760c8`, `dbos/internal/sysdb/system_database.go` `ResumeWorkflows` lines 2440-2498):

```sql
-- Abridged illustration (source: dbos/internal/sysdb/system_database.go lines 2440-2498, commit ab56911)
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

WebSocket fork requests (`fork_workflow`) and data-plane fork mutations apply identical workflow initialization semantics. The data-plane client invokes `client.ForkWorkflow` (`internal/dataplane/client.go`), creating a new workflow record with state copied up to the requested step index and enqueued for execution on `_dbos_internal_queue`.

### Durable sleep and message receive during cancellation

When a running workflow is cancelled via the data plane while suspended in a durable sleep (`sleep`)
or waiting on an event message (`recv`):

* **Go SDK** (`dbos/workflow.go:4172-4202`, `3482-3575`): The worker process sleeps for the remaining
  duration via an in-memory timer. On timer expiration, the sleep function returns without checking the
  system database. Cancellation is detected only at the next checkpointed step boundary or when attempting
  to finalize the workflow outcome (`UpdateWorkflowOutcome`).
* **Python SDK** (`dbos/_dbos.py:1871-1901`, `dbos/_sys_db.py:3647-3670`): The worker thread sleeps
  until the local duration completes (`time.sleep` or `asyncio.sleep`). Recheck intervals in `recv`
  poll exclusively for incoming notifications on `dbos.notifications`. On wakeup, the workflow continues
  and detects cancellation at the subsequent step boundary.
* **TypeScript SDK** (`src/system_database.ts:2889-2897`, `3065`): The worker sleeps locally until the
  target timestamp. However, immediately upon timer expiration in `durableSleepms` and upon event resolution
  in `recv`, the SDK explicitly invokes `checkIfCanceled(workflowID)`. If the row was updated to `CANCELLED`
  in `dbos.workflow_status` during the sleep window, the executor immediately throws `DBOSWorkflowCancelledError`
  and aborts execution before running any subsequent user steps.

### Served-from auditing

Internal routing tracks request dispatch attribution (`internal/router.ServedFromTracker`)
between live WebSocket executors and data-plane fallback reads. Surfacing `served_from`
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
