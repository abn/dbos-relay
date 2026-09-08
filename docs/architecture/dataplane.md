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
  through the official DBOS Go SDK client library. Relay never issues raw SQL queries (`SELECT * FROM dbos.*`).
* **Default read-only**: Data plane connections default to `read` mode. Mutation operations (cancellations,
  restarts, resume) are rejected unless the operator explicitly opts in to `read-write` mode.
* **Executor precedence**: When a live executor is connected, requests are routed to the executor over
  WebSocket. The data plane acts as a fallback when no executors are reachable.
* **Auditable responses**: When a query is served via database fallback, Relay marks the response with
  `served_from: database` in metadata and response headers.

## Configuration

Data-plane access is configured per-application in `relay.yaml`:

```yaml
data_plane:
  order-service:
    connection_url: "postgres://dbos_read:secret@db.internal:5432/order_system_db"
    mode: "read"
    statement_timeout_secs: 10
    max_connections: 5
```

Configuration parameters:

* `connection_url`: PostgreSQL connection string for the application system database.
* `mode`: Access mode (`read` or `read-write`). Defaults to `read`.
* `statement_timeout_secs`: Enforced statement timeout bounding queries.
* `max_connections`: Maximum pool size allocated for this application's data plane client.

## Wire and data-plane parity

Administrative operations behave identically whether dispatched over WebSocket to a connected
executor or applied directly through the SDK client data plane.

### Cancellation semantics

WebSocket cancellation requests and data-plane cancellation calls apply the exact same database update:

```sql
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

WebSocket resume requests and data-plane resume calls transition workflows back into the queue table:

```sql
UPDATE dbos.workflow_status
SET status = 'ENQUEUED',
    queue_name = '_dbos_internal_queue',
    recovery_attempts = 0,
    started_at_epoch_ms = NULL,
    completed_at = NULL
WHERE workflow_uuid = $1
  AND status IN ('CANCELLED', 'ERROR');
```

Any live executor polling `_dbos_internal_queue` via `DequeueWorkflows` picks up the resumed workflow
and executes remaining steps.

### Served-from auditing

Every response served through the data plane is tagged in response headers and metrics with
`served_from: database` (compared to `served_from: executor` for WebSocket dispatches). This covers
both read queries and administrative mutations (cancel, resume, restart).

## Operational metrics and alerting

Relay records the source that fulfilled each request in the `relay_requests_served_total` metric
with the `served_from` label (`executor` or `database`).

If the ratio of requests served via database fallback exceeds 50% over a 5-minute window,
the `RelayHighDatabaseFallbackRatio` Prometheus alert rule fires:

```yaml
# deploy/observability/prometheus/relay-alerts.yaml
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
