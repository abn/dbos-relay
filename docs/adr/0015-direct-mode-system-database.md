---
type: Decision
title: ADR 0015 - Direct mode through the system database
description: Make the MIT external client a first-class control-plane integration, with explicit safety and liveness boundaries.
status: accepted
---

# ADR 0015 - Direct mode through the system database

## Status

Accepted. Implementation and the conformance matrix land in a separate change.

## Context

[ADR 0014](0014-connector-optional-positioning.md) withdraws the drop-in
promise for post-split SDK releases and keeps Attached mode for pre-split SDKs
and Java. That leaves a gap: an application on a post-split SDK still needs a
control plane, and the connector that would carry it is proprietary.

The application does not need the connector to be durable. DBOS Transact
writes every workflow, step, output, event, stream, queue, schedule, and
application version to the application's system database. The connector is a
transport for operations over that state. The state itself is open:

- The Go, Python, and TypeScript SDKs publish an MIT external client,
  `dbos.Client` in Go and `DBOSClient` in Python and TypeScript, with enqueue,
  send, cancel, resume, fork, rewind, workflow queries, queues, schedules, and
  application versions.
- The first post-split releases are at system database schema 123: Go `v1.7.0`,
  Python `3.3.0`, and TypeScript `5.3.0`. The current releases are at schema
  125: Go `v1.8.0`, Python `3.4.0`, and TypeScript `5.4.0`.
- Go SDK `v1.7.0` requires schema 123, tolerates newer schemas, reads the
  out-of-line payload tables, supports PostgreSQL and SQLite system databases,
  and provides the MIT `dbos/internals` seam that exposes
  `SystemDatabase.ReenqueueForRecovery`. The SDK core runs the database-backed
  scheduler in a launched application; the external client creates and manages
  the schedule rows. It writes portable JSON when the input is a
  `PortableWorkflowArgs` value.
- Go SDK `v1.8.0` adds `WithEnqueuePortableWorkflow` as a convenience for the
  same portable JSON format, but it requires schema 125, so pinning it would
  strand the first post-split cohort.

Relay already permits this client for optional outage reads under
[ADR 0004](0004-data-plane-via-sdk-client.md). The change is one of role, not
of access mechanism: the client moves from a fallback to a first-class
integration.

The remaining gap is liveness. The database has no executor heartbeat. Relay
can see workflow rows and their executor IDs, but it cannot tell a dead
executor from a long durable sleep without an external signal. Recovery
therefore needs a deployment-level input.

## Decision

Relay supports a **Direct mode** in which it observes and controls an
application through the application's own system database, using the official
MIT Go client. Direct mode is a first-class mode, not a read-only fallback.

The rules are:

1. **Same access mechanism.** Direct mode uses the official Go client and the
   MIT `dbos/internals` seam. It does not issue raw SQL against `dbos.*` tables
   and does not depend on the proprietary connector. [ADR 0004](0004-data-plane-via-sdk-client.md)
   remains the access rule. This record supersedes ADR 0004 points 4 and 5 for
   Direct mode: the operation set is wider than the original six, and portable
   JSON allows enqueue, send, and event writes that the original record
   deferred. The legacy fallback keeps the original scope until it is removed.
2. **Read-only by default.** A Direct connection is read-only unless the
   operator explicitly opts into read-write for that application. In read-write
   mode the allowlist is the client's operations: cancel, resume, fork, rewind,
   delete, enqueue, send, set event, schedule changes, queue changes, and
   recovery. The operator's read-write choice is the gate for Direct mutations;
   the legacy condition that mutations are allowed only when no executor is
   connected does not apply to Direct mode. There is no DDL, no migration, and
   no query Relay invents.
3. **Recovery needs a liveness signal.** Relay calls
   `ReenqueueForRecovery` for a dead executor only when a deployment-level
   signal says the executor is gone. The signal comes from a Kubernetes
   operator watching pod lifecycle or from a small `relay-probe` process that
   shares the application process lifetime and reports its executor ID.
   Without a signal, Direct mode observes and controls but does not
   automatically recover, and it says so.
4. **A bounded schema window.** Direct mode v1 pins Go SDK `v1.7.0`, which
   requires schema 123 and tolerates newer. The supported window is schema 123
   through 125, which covers both post-split cohorts. The prototypes cover a
   123 recovery run and a 125 read; the conformance matrix adds a 123 cell and
   a 125 cell, and 124 is covered by the client's migration tolerance and its
   index-only migrations. Newer schemas are tolerated by the client's migration
   check until the pin is raised, but they are not claimed as tested. A schema
   below 123 is refused with a problem response, not read with guessed columns.
   Applications below the floor use Attached mode.
5. **Portable payloads for writes.** Cross-language writes use DBOS portable
   JSON. The v1.7.0 pin uses `PortableWorkflowArgs`; a later pin can use
   `WithEnqueuePortableWorkflow`. Relay never re-serializes a payload in another
   runtime's native format and never unpickles untrusted data. Values it cannot
   decode safely are displayed as opaque.
6. **No per-language SDKs.** Direct mode is implemented once in Relay's Go
   server. The optional probe and operator are deployment components, not
   application libraries. Relay does not publish Python, TypeScript, or Java
   packages.
7. **The client is a watched dependency.** The implementation pins the Go SDK,
   vendors its source for reproducible builds, and runs a canary test against
   every new release before raising the pin. The project asks upstream to keep
   the external client MIT and the system database migrations public. If that
   changes, the contingency is a fork of the last MIT client or a narrow,
   client-mediated adapter, not a return to four connectors.

### Why v1.7.0 rather than v1.8.0

The current client is not always the right client for a control plane. A
client that requires schema 125 cannot read the first post-split releases at
schema 123, and those releases have no connector to fall back to. v1.7.0
requires 123 and tolerates 125, so one client covers both cohorts. The cost is
that v1.8.0's `WithEnqueuePortableWorkflow` option is unavailable; v1.7.0
already supports the underlying portable JSON through `PortableWorkflowArgs`.

The pin is raised when the schema-123 cohort ages out of the support window,
or when a needed fix exists only in a later client and the new client's floor
does not strand a supported generation. Raising the pin is a scoped change with
a fixture and a conformance run, not a routine dependency bump.

### Schema floor and the legacy fallback

The current data plane pins Go SDK `v1.3.0` to keep schema-107 applications
working. Direct mode uses `v1.7.0`, which requires schema 123. The two clients
cannot coexist in one Go module.

Direct mode therefore retires the `v1.3.0` fallback. The consequences are
deliberate:

- Applications at schema 123 through 125, including both post-split cohorts,
  are served by Direct mode.
- Applications below schema 123 are served by Attached mode. They keep the
  connector they already have.
- The out-of-line payload gap recorded in
  [ADR 0013](0013-out-of-line-workflow-payloads.md) does not apply to Direct
  mode. It remains only for the legacy fallback until that fallback is removed.
- The sample applications and the data-plane tests move to the supported
  schema generations and Direct mode in a separate implementation change. The
  examples that exist to prove Attached mode keep their pinned SDKs.

### What Direct mode does not claim

- Live CPU, memory, and in-flight executor telemetry. Use a probe,
  OpenTelemetry, or Attached mode.
- Java applications at schema 114, the current Java `1.2.0` generation. They
  use Attached mode until Java's schema aligns with the Direct window or an
  adapter exists. Schema 19 belongs to Java `0.8.0` and older.
- SQLite as an operator-facing production backend in v1. The Go client supports
  it and the hermetic tests use it, but the operator surface and the
  multi-language story target PostgreSQL.
- Non-portable serializers for writes. They are read and displayed, not
  re-encoded.
- Automatic recovery without a liveness signal.
- Any behaviour of the proprietary connector or the proprietary server. None is
  inspected or tested.

## Evidence

Two prototypes run on 2026-11-06 against the real Go SDK and an embedded
SQLite system database proved the path without the connector. The implementation
change turns them into reproducible tests in the repository.

**Recovery on schema 123 with `v1.7.0`:**

1. A workflow ran one durable step and entered a durable sleep.
2. The process was killed with `SIGKILL`. The workflow row remained `PENDING`
   with its completed step recorded and executor ID `exec-dead`.
3. A separate process opened the database through the Go client and called
   `ReenqueueForRecovery` for `exec-dead`. The row moved to `ENQUEUED` on
   `_dbos_internal_queue`.
4. A fresh executor with a different ID launched, dequeued the workflow, did
   not re-run the completed step, and completed with output `done`.

**Cross-generation read with `v1.7.0`:** a schema-125 database created by
`v1.8.0` was opened by the `v1.7.0` client and its workflow rows were read
successfully. This is the evidence for the 123-through-125 window.

The prototypes are not a substitute for the conformance test. They establish
that the operation is possible with the public MIT client and no connector. The
implementation change adds the hermetic tests to the repository, where they run
in CI.

## Consequences

- Relay gains a control-plane path that does not depend on the connector and
  works across languages, because the schema is shared.
- The data plane becomes a primary code path. It needs the same test rigour,
  observability, and documentation as the WebSocket path.
- The schema floor rises from 107 to 123. This is an intentional trade: a
  client that covers both post-split cohorts and reads out-of-line payloads is
  worth more than a fallback for applications that can still use Attached mode.
- Relay's dependency on the Go client becomes load-bearing. The release watch,
  the vendored source, and the upstream ask are part of the decision, not
  optional hygiene.
- The dashboard and REST API can serve a fleet with zero connected executors.
  The "no live executors" state becomes a normal state rather than an outage.
- Recovery is only as good as the liveness signal. The probe and operator are
  therefore first-class deliverables, and the dashboard must distinguish
  "no signal configured" from "executor dead".

## Alternatives considered

1. **Pin `v1.8.0` and support schema 125 only.** Rejected. It strands the first
   post-split cohort at schema 123, which has no connector to fall back to.
2. **Run two clients, `v1.7.0` and `v1.8.0`, in separate processes.** Rejected
   for v1. One client already covers both cohorts. Two clients add a second
   binary, a second connection pool, and a second failure mode for no capability
   the first version needs.
3. **Use raw SQL for the whole data plane.** Rejected. The MIT client already
   exposes the operations, keeps the provenance clean, and avoids a second
   implementation of DBOS semantics. A narrow adapter remains the contingency
   if the client becomes unavailable, and it stays client-mediated or a fork of
   the MIT client.
4. **Build surrogate connectors.** Rejected as the foundation in
   [ADR 0014](0014-connector-optional-positioning.md). They remain a user-side
   option.
5. **Require Attached mode for recovery.** Rejected. Recovery is the reason a
   control plane exists; leaving it to a proprietary client would make Direct
   mode a dashboard rather than a control plane.

## Sources

- `dbos-inc/dbos-transact-golang` tag `v1.7.0`, commit
  `339705771d7e198c5bac9e213903820c0575e6c1`, published 2026-10-15. The module
  contains `dbos/enterprise.go` and no Conductor client; `dbos/dbos.go` defines
  `Client` and `NewClient`; `dbos/workflow.go` defines `PortableWorkflowArgs`
  handling; `dbos/internal/sysdb/system_database.go` defines
  `ReenqueueForRecovery`; the migration list ends at 123.
- `dbos-inc/dbos-transact-golang` tag `v1.8.0`, commit
  `f543516f2014d0c3d80a08dbe44fcc545c76a183`, published 2026-10-28. The
  migration list ends at 125 and `WithEnqueuePortableWorkflow` is present.
- `dbos-inc/dbos-transact-py` `3.3.0` and `3.4.0`: `dbos/_enterprise.py` is the
  loader, `dbos/_client.py` contains `DBOSClient`, and the migration lists end
  at 123 and 125 respectively.
- `dbos-inc/dbos-transact-ts` `5.3.0` and `5.4.0`: `dist/src/enterprise.js` is
  the loader, `dist/src/client.js` contains `DBOSClient`, and the migration
  lists end at 123 and 125 respectively.
- `dbos-inc/dbos-transact-java` `1.2.0`: `MINIMUM_SYSDB_VERSION` is 111 and the
  migration list ends at 114. The Conductor tree is still public.
