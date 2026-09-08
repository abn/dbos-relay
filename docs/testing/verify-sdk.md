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
| Conformance suite + `dbosctl` script via socket | required | required | required | required |
| Data plane read: status, list, steps; payloads pass through with serialisation tag | required | required | required | may lag one iteration |
| Field parity: same workflow via socket and data plane, byte-equal after normalisation | required | required | required | may lag |
| Chaos, real timers: SIGKILL executor, recovery on survivor, workflow completes exactly once | required | required | required | may lag |
| Data-plane cancel and resume while executor is down; restarted executor honours both | required | required | required | may lag |
| Fork via data plane to a live version; executor dequeues and runs it | required | required | required | may lag |

## Cell definitions and test requirements

### 1. Socket connection and executor presence
The sample application launches and initiates a WebSocket connection to Relay at
`/websocket/{appName}/{conductorKey}`. Relay registers the executor in its live registry,
updates its heartbeat lease, and surfaces the executor in the `GET /v2/orgs/{org}/apps/{app}/executors`
API endpoint.

### 2. Conductor conformance and CLI execution
The upstream `dbosctl` CLI commands and Conductor protocol test batteries run against the
connected executor over WebSocket. The executor correctly responds to workflow execution,
step listing, and telemetry probes.

### 3. Data plane read and serialization preservation
Relay reads workflow status, step lists, and outputs directly from the application system
database using the SDK client data plane. Complex outputs (Python pickle/JSON, TypeScript JSON,
Go JSON/gob) pass through Relay without mutation, preserving their original `serialization` tag.

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

## Lagging cells policy

Cells marked "may lag" (e.g. Java data-plane integration during initial rollout) are explicitly
permitted to lag by at most one iteration. However, lagging tests must never be silently omitted:
the test runner must explicitly report them as skipped by name in its execution summary. Any
required cell without a passing test causes the test runner to fail.
