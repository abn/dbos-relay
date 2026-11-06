---
type: Reference
title: Multi-SDK verification matrix
description: Conformance and data-plane verification matrix across Python, TypeScript, Go, and Java SDKs.
status: draft
---

# Multi-SDK verification matrix

Relay is tested against applications built with the official DBOS Transact SDKs.
This matrix defines the required verification gates across supported SDK runtimes:
Python, TypeScript, Go, and Java.

## Verification matrix

| Cell | Python | TypeScript | Go | Java |
|---|---|---|---|---|
| Sample app connects to Relay over the socket; appears in executors | required | required | required | required |
| Conformance suite + REST endpoint probes | required | required | required | required |
| Data plane read: status, list, steps; payloads preserved | required | required | required | required |
| Field parity: same workflow via socket and data plane, byte-equal after normalisation | required | required | required | Attached only (schema 114) |
| Chaos, real timers: SIGKILL executor, recovery on survivor, workflow completes exactly once | required | required | required | required |
| Data-plane cancel and resume while executor is down; restarted executor honours both | required | required | required | Attached only (schema 114) |
| Fork via data plane to a live version; executor dequeues and runs it | required | required | required | required |

The data-plane rows are exercised by the Direct mode suite in
`internal/dataplane`. The Attached matrix keeps its sample applications on
pre-split SDKs and skips those rows by default. The multi-SDK matrix extension
adds cross-language Direct cells.

## Cell definitions and test requirements

### 1. Socket connection and executor presence
The sample application launches and initiates a WebSocket connection to Relay at
`/websocket/{appName}/{conductorKey}`. Relay registers the executor in its live registry,
updates its heartbeat lease, and surfaces the executor in the `GET /v2/orgs/{org}/apps/{app}/executors`
API endpoint.

### 2. Conductor conformance and REST endpoint probes
The Conductor protocol test batteries and HTTP REST endpoint probes run against the
connected executor and application. The executor correctly responds to workflow execution,
step listing, and telemetry probes. Note that the upstream CLI client binary is not exercised
directly in this test suite.

### 3. Data plane read and payload preservation
Relay reads workflow status, step lists, and outputs directly from the application system
database using the SDK client data plane. Complex outputs (Python pickle/JSON, TypeScript JSON,
Go JSON/gob) pass through Relay without mutation, preserving payload bytes verbatim.

### 4. Field parity between socket and database
The same completed workflow is retrieved twice: once over the WebSocket protocol from a live
executor, and once via data-plane fallback against the system database. After normalizing
timestamps and ephemeral request identifiers, all attributes and payloads must be byte-equal.

### 5. Chaos recovery under real timers
A workflow executes across multiple steps. The active executor process is terminated with
`SIGKILL`. Relay detects the heartbeat timeout, marks the executor dead, and dispatches
recovery to a surviving executor. The workflow resumes from its last checkpoint and completes
exactly once.

### 6. Offline data-plane cancel and resume
While zero executors are running, administrative cancellation and resume operations are
issued through the data plane. When an executor restarts, it inspects the system database
and honours the updated workflow status.

### 7. Data-plane fork and live dequeue
A workflow is forked via data-plane API targeting a specific application version. An executor
running that version dequeues the workflow from `_dbos_internal_queue`, executes the remaining
steps, and records the outcome.

## Data-plane cells in the Attached matrix

Cells 4, 6, and 7 are skipped in the Attached matrix by default. Its sample
applications stay on pre-split SDKs, and the current Direct client requires
schema 123. Direct mode coverage runs in `internal/dataplane` against a
hermetic SQLite database and live PostgreSQL. The multi-SDK matrix extension
adds schema 123 and 125 cells across languages. Skipped cells are reported by
name in the execution summary; a skipped cell is not a passing cell.

## SDK version ceiling

The sample applications in `examples/` are pinned inside the supported SDK range, and `tests/verifysdk/sdk_ceiling_test.go` enforces it: the test reads the example manifests and fails when a pin has moved past the ceiling. That check reads files only, so it runs offline and fast as part of `make test`, and a future SDK bump cannot leave the supported window silently. The ceiling itself, and the evidence behind it, lives in [Supported SDKs](../usage/supported-sdks.md).

### Payload parity at the ceiling

Direct mode uses Go SDK v1.7.0, which reads the out-of-line
`workflow_input` and `workflow_output` tables introduced in v1.4.0 (migration
109). The payload blind spot recorded in
[ADR 0013](../adr/0013-out-of-line-workflow-payloads.md) applied to the
retired v1.3.0 fallback and does not apply to Direct mode. The Attached matrix
skips the data-plane parity cell; the live PostgreSQL data-plane test asserts
payload parity against the real client, and the hermetic SQLite test covers
workflow and step reads.

### Direct mode cross-language suite

`tests/direct` runs the official Python `3.4.0` and TypeScript `5.4.0` SDKs
against a PostgreSQL system database, then reads the result through Relay's
Direct client. It asserts status, output, steps, recovery, and cancel. The
Python fixture also runs against SQLite, which needs no server. The fixtures use
the portable JSON serializer so the Go client can decode cross-language
payloads. Native Python pickle and TypeScript superjson payloads are outside
this suite and outside the cross-language payload guarantee.
