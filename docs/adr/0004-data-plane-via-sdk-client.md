---
type: Decision
title: ADR 0004 - Optional data-plane access via SDK client
description: Permit optional data-plane reads and mutations through the supported SDK client library.
status: stable
---

# ADR 0004 - Optional data-plane access via SDK client

## Status

Accepted.

## Context

The initial architectural invariant ("never touch an application database") was
inherited from the operational security model of multi-tenant hosted Conductor.
In that hosted SaaS environment, Conductor operates outside customer network
perimeters and relies strictly on outbound WebSocket connections from executors.

In self-hosted deployments inside an operator's private perimeter, this rule
imposes severe operational limitations:

1. **Outage invisibility**: When all executors for an application crash or
   terminate, every API read returns HTTP 503 Service Unavailable. The
   operator is blind to application state at the exact moment incident triage
   is needed.
2. **Execution contention**: Heavy analytical queries and fleet aggregations
   (needs-attention summaries, version mapping, token expenditure roll-ups)
   must fan out over WebSocket connections to active executor processes that
   are actively executing mission-critical workflow logic.

The DBOS Transact SDKs ship `DBOSClient`, an officially supported, public
external-process client designed for accessing the system database without
launching an executor runtime. It provides APIs for workflow inspection,
event retrieval, message dispatch, queue management, cancellation, resumption,
and forks.

## Decision

Relay allows an optional data-plane connection using the official Go SDK
client library (`github.com/dbos-inc/dbos-transact-golang/dbos`) for applications
where the operator has explicitly configured data-plane credentials.

Direct raw SQL queries against `dbos.*` tables remain strictly forbidden. All
database interaction must pass through the SDK client.

The implementation follows these rules:

1. **Isolation in `internal/dataplane`**: A dedicated package wraps the Go SDK
   client behind a clean interface. It runs in-process today, with no external
   RPC overhead, while maintaining the abstraction boundary for future isolation.
2. **Explicit per-application opt-in**: Configured via a `data_plane` block
   in `relay.yaml` containing the database URL, optional read-replica URL, and
   mode (`read` or `read-write`). With no block present, Relay operates in pure
   WebSocket mode with identical drop-in semantics and zero behavioral drift.
3. **Read-only by default**: The default mode is `read`. Operators may
   explicitly specify `read-write` to permit mutations (such as
   `CancelWorkflow`, `ResumeWorkflow`, and `ForkWorkflow`) when no executor
   is connected.
4. **Scoped v1 operations**: Version 1 of the data plane supports operations
   that do not require application-specific compiled types or schemas:
   - `GetWorkflow` (`client.ListWorkflows` with workflow ID filter)
   - `ListWorkflows` (`client.ListWorkflows`)
   - `GetWorkflowSteps` (`client.GetWorkflowSteps`)
   - `CancelWorkflow` (`client.CancelWorkflow`)
   - `ResumeWorkflow` (`client.ResumeWorkflow`)
   - `ForkWorkflow` (`client.ForkWorkflow`)
5. **Deferred operations and serialization boundary**: Inter-workflow communication
   and event retrieval (`GetEvent`, `Send`, `Enqueue`, and `ReadStream`) are
   deferred in v1 (returning HTTP 501 Not Implemented or HTTP 503). The
   architectural blocker is not Go generic typing, but cross-language
   serialization mismatch. If Relay writes a message using `dbos.Send` or
   `dbos.Enqueue`, it serializes payloads using Go's serializer (such as gob or
   Go JSON wrapper). When an executor written in Python, TypeScript, or Java
   dequeues that message, it crashes trying to deserialize foreign bytes with its
   own language runtime serializer (such as Python pickle or custom JSON). Mutating or
   reading application event queues directly cannot proceed without either live
   executor mediation or an upstream SDK API that accepts and exposes raw,
   uninterpreted bytes with language tags. This forms an explicit upstream feature ask:
   raw-bytes or language-tagged variants of `Send`, `Enqueue`, and `SetWorkflowEvent`.
6. **Schema safety and migration immunity**: The SDK client constructor
   (`dbos.NewClient`) hardcodes `SkipMigrations: true`, ensuring Relay never runs
   migrations against customer application databases. When connecting, it only
   calls `VerifyMigrations`. If the target database schema version is newer than
   Relay's Go SDK version expects, it operates safely without error. If the
   schema version is older, it fails fast with `*models.UnmigratedDatabaseError`
   and refuses connection, leaving the schema untouched.
7. **Cross-language payload handling**: Status, listing, and step metadata share
   a unified database schema across all SDK languages. Payload columns (`input`,
   `output`, `error`) are treated as opaque strings or JSON text. For standard
   JSON, Relay forwards payloads without double-marshaling. For non-Go or
   non-JSON serializers (such as Python pickle or custom serializers), the Go SDK
   client falls back to raw string preservation upon decoding errors, and Relay
   emits them verbatim with wire parity, never mangling or failing list/step queries.
8. **Routing strategy**: WebSocket routing is preferred when healthy executors
   are connected. When no healthy executor is available, requests fall back to
   the data-plane. Heavy fleet-wide aggregations prefer the data-plane when
   configured.
9. **Observability markers**: External response headers (`X-Relay-Served-From`)
   and Prometheus metrics (`relay_requests_served_total` with `served_from`
   label) are planned for the Scale and Observability tier to allow clients to
   distinguish database reads from live executor reports.
10. **Connection guardrails**: Data-plane pools enforce bounded connections (`max_connections`)
    and per-query statement timeouts (`statement_timeout_secs`). Additional scale-tier guardrails
    (rate limits, read replica pool routing, and dedicated OpenMetrics counters and latency
    histograms) are planned for the Scale and Observability tier.
11. **Version pinning and consistency**: The Go SDK version is strictly pinned
    and validated against protocol tests to guarantee representation parity
    between socket reads and client reads.

## Consequences

- Eliminates the 503 visibility gap during application outages.
- Enables human-in-the-loop actions via client message dispatch ahead of
  upstream wire protocol additions.
- Protects live executors from heavy fleet aggregation queries.
- Applications without a configured `data_plane` block retain complete drop-in
  compatibility with no database access.
