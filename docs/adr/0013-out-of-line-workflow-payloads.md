---
type: Decision
title: ADR 0013 - Out-of-line workflow payloads in the data plane
description: Keep the Go SDK data-plane client at v1.3.0 and record out-of-line payload reads as a capability gap.
status: accepted
---

# ADR 0013 - Out-of-line workflow payloads in the data plane

## Status

Accepted.

## Context

DBOS Transact SDK v5 adds migration `109_workflow_payload_tables`, which moves
workflow inputs and outputs off `workflow_status` into separate tables,
`workflow_input` and `workflow_output`. The migration carries the comment
"Payloads move off workflow_status so a status update no longer rewrites a
large input" (`dbos-transact-ts` commit `749a4d4`,
`src/sysdb_migrations/internal/migrations.ts`, migration
`109_workflow_payload_tables`). The Python SDK has the same layout
(`dbos-transact-py` commit `2b93e14`, `dbos/_migration.py`). The TypeScript
v5.1.10 migration list ends at `122_workflow_status_deadline_index`, and the
Python source reaches migration 123.

Relay's data-plane client is constructed with `dbos.NewClient`
(`internal/dataplane/client.go`). `NewClient` hardcodes `SkipMigrations: true`,
which routes to `VerifyMigrations` (`dbos/dbos.go` and
`dbos/internal/sysdb/system_database.go`, commit
`ab56911fdd78552e1e7fe648cff7c831a1e760c8`). A database behind the version a
client requires is refused with `*models.UnmigratedDatabaseError`; a database
ahead is tolerated. No public `ClientConfig` field relaxes this.

The pinned Go client (`github.com/dbos-inc/dbos-transact-golang v1.3.0`, in
`go.mod`) requires schema 107 and reads `inputs`, `output`, and `error` directly
from `workflow_status`. Its source contains no reference to `workflow_input` or
`workflow_output` (`dbos/internal/sysdb/system_database.go`, commit
`ab56911fdd78552e1e7fe648cff7c831a1e760c8`). Later releases read out of line:

* `v1.4.0` requires schema 113 and reads input, output, and error through
  `COALESCE(workflow_input.inputs, workflow_status.inputs)` and
  `COALESCE(workflow_output.output, workflow_status.output)` with `LEFT JOIN`s
  (`dbos/internal/sysdb/system_database.go`, commit
  `5387dd34db00b1762a2367ab4bf1de935afa09f7`).
* `v1.5.0` (published 2026-09-29) requires schema 121 and reads the same fields
  the same way (`dbos/internal/sysdb/system_database.go`, commit
  `7e9d7c1658ae45ddfe068315a48fe8dc509d7752`).

No single Go client release both reads out-of-line payloads and tolerates schema
107. Out-of-line reading starts at `v1.4.0`, which already requires 113.
Raising the pin would take the whole data plane offline, status, list, and step
reads included, for every application below the new floor, and the Go sample
application itself pins `v1.3.0` at schema 107 (`examples/golang/go.mod`). That
conflicts with the invariant that an unmodified application works by pointing
its Conductor URL at Relay.

The gap is narrow in practice. The data plane is an opt-in fallback used only
when no executor is connected (`internal/router/router.go`). The Conductor
protocol answers payload requests with the executor's own SDK, so a v5 executor
returns out-of-line payloads correctly. Fleet aggregation prefers the data
plane, but the aggregate messages carry no payloads. What remains is the
workflow input, output, and error payloads, for out-of-line applications, only
during an outage.

## Decision

Keep the Go SDK data-plane client pinned at
`github.com/dbos-inc/dbos-transact-golang v1.3.0`. Treat reads of out-of-line
workflow payloads as a documented data-plane capability gap. Do not raise the
schema floor.

The alternatives were rejected:

1. **Upgrade to `v1.4.0` or `v1.5.0`.** This raises the floor to 113 or 121 and
   strands every application below it, taking status, list, and step reads
   offline as well. That is a worse failure than empty payloads on a fallback
   read.
2. **A two-generation reader** through a separate process or a vendored fork.
   This adds a second runtime and a new operational surface for a field that is
   absent only during an outage, and it conflicts with the executor-sidecar
   rejection in [ADR 0005](0005-rejected-executor-sidecar.md).
3. **Reading `dbos.workflow_input` with raw SQL.** Forbidden by
   [ADR 0004](0004-data-plane-via-sdk-client.md) and the project invariants;
   all database access passes through the SDK client.
4. **Compatibility views.** These require DDL against the application database,
   which is an application-side change.

## Consequences

- The data plane keeps working for every application the pinned client can
  reach, including the Go sample at schema 107.
- Workflow input, output, and error payloads are empty for out-of-line
  applications when a read is served from the data plane during an outage. The
  gap is stated in [Data-plane access via SDK client](../architecture/dataplane.md).
- The decision is revisited when an upgrade can happen in one step to a client
  whose required schema no longer strands any in-scope application, that is,
  once the minimum schema among supported applications is at or above the
  candidate client's floor. It is also revisited when an operator opts into a
  higher floor for a fleet entirely on v5.
- Upstream feature ask: an SDK client option to tolerate older schemas, or a
  version-agnostic payload read that does not raise the floor. Following the
  rule that an application change must never be required, the request travels
  upstream while Relay keeps working without it.
