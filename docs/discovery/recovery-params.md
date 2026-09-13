---
type: Reference
title: Recovery timing parameters and application configuration
description: Timing defaults, per-application overrides, state transitions, units, and recovery engine requirements.
status: draft
---

# Recovery timing parameters and application configuration

Relay provides automatic workflow recovery when executors fail, crash, or
disconnect. This document specifies the timing parameters, lifecycle state
transitions, per-application configuration overrides, unit conventions, and
architectural constraints governing the recovery engine.

All parameters and behaviours in this document are derived from permitted public
sources under clean-room rules.

## Provenance and clean-room citations

Every specification in this document is cited from permitted sources:

* **DBOS public documentation**:
  * Workflow recovery semantics and 60-second default timeout:
    `https://docs.dbos.dev/production/workflow-recovery` (fetched 2026-09-08)
  * Workflow retention policies and global timeout:
    `https://docs.dbos.dev/production/retention` (fetched 2026-09-08)

* **Conductor OpenAPI specification**:
  * Vendored contract: `api/spec/openapi.json` (SHA256: `b5dc31eb29686a84fe0390a7446b5acdbc0dd05846cc94a746de649b92880722`)
  * Schemas: `Application`, `PatchAppInputBody`, `PutAppInputBody`, `Executor`
  * Operations: `updateApp` (`PATCH /v2/orgs/{orgName}/apps/{appName}`),
    `getApp` (`GET /v2/orgs/{orgName}/apps/{appName}`),
    `listExecutors` (`GET /v2/orgs/{orgName}/apps/{appName}/executors`)

* **dbos-transact-go**:
  * Repository: `https://github.com/dbos-inc/dbos-transact-go`
  * Commit: `ab56911fdd78552e1e7fe648cff7c831a1e760c8` (MIT)
  * Heartbeat and timeouts: `dbos/conductor.go` (lines 28-35: `_PING_INTERVAL = 20 * time.Second`,
    `_PING_TIMEOUT = 30 * time.Second // Should be slightly greater than server's executorPingWait (25s)`,
    `_INITIAL_RECONNECT_WAIT = 1 * time.Second`, `_MAX_RECONNECT_WAIT = 30 * time.Second`,
    `_HANDSHAKE_TIMEOUT = 10 * time.Second`, `_WRITE_DEADLINE = 5 * time.Second`)
  * Connection management and ping loop: `dbos/conductor.go` (lines 285-357)
  * Recovery request dispatch handling: `dbos/conductor.go` (lines 470-503)
  * System database re-enqueue: `dbos/recovery.go` (lines 8-22)
  * Retention request and body schemas: `dbos/conductor_protocol.go` (lines 514-532)

* **dbos-transact-py**:
  * Repository: `https://github.com/dbos-inc/dbos-transact-py`
  * Commit: `833794f7a1138bacf75ff6d88647a33eb5e35e52` (MIT)
  * Ping interval and pong timeout: `dbos/_conductor/conductor.py` (lines 54-55:
    `self.ping_interval = 20`, `self.ping_timeout = 15`)
  * Keepalive thread loop: `dbos/_conductor/conductor.py` (lines 65-95)
  * Recovery request handling: `dbos/_conductor/conductor.py` (lines 148-165)
  * Retention and global timeout dispatch: `dbos/_conductor/conductor.py` (lines 600-630)

* **dbos-transact-ts**:
  * Repository: `https://github.com/dbos-inc/dbos-transact-ts`
  * Commit: `d8c4974cca6cc84b296f3b8edfbbb41627ddd47e` (MIT)
  * Ping period and timeout: `src/conductor/conductor.ts` (lines 28-32:
    `pingPeriodMs = 20000`, `pingTimeoutMs = 15000`, `reconnectDelayMs = 1000`,
    `handshakeTimeout = 5000`)
  * Ping interval timer and socket reset: `src/conductor/conductor.ts` (lines 48-89)
  * Recovery handler: `src/conductor/conductor.ts` (lines 150-161)

* **dbos-transact-java**:
  * Repository: `https://github.com/dbos-inc/dbos-transact-java`
  * Commit: `1248174f393bd97f9973ec83cbc6e42b6e319ed1` (MIT)
  * Builder defaults: `transact/src/main/java/dev/dbos/transact/conductor/Conductor.java`
    (lines 88-90, 525: `pingPeriodMs = 20000`, `pingTimeoutMs = 15000`, `reconnectDelayMs = 1000`)

* **dbos-ctl**:
  * Repository: `https://github.com/dbos-inc/dbos-ctl`
  * Commit: `9d14ed3f0ccddb84cd3390e0bddbcfb9ea9a32a6` (MIT)
  * App fields display: `internal/cli/app.go` (lines 382-396)
  * App update command and flag tests: `internal/cli/app_test.go` (lines 587-616)

## Default timing parameters

The table below catalogues all default timing parameters governing executor
connections and recovery.

| Parameter | Default Value | Measured In | Initiator / Owner | Permitted Source |
| --- | --- | --- | --- | --- |
| Server ping interval | 20 seconds | seconds (`s`) | Control plane (Relay) | `internal/hub/conn.go:18`, `docs/protocol/executor-ws.md:221` |
| Client ping interval | 20 seconds | seconds (`s` or `ms`) | Executor SDK | Go SDK (`dbos/conductor.go:28`), Python SDK (`conductor.py:54`), TS SDK (`conductor.ts:28`), Java SDK (`Conductor.java:525`) |
| Server ping wait (`executorPingWait`) | 25 seconds | seconds (`s`) | Control plane (Relay) | Go SDK (`dbos/conductor.go:29`) |
| Client pong timeout | 15s (Py, TS, Java) / 30s (Go) | seconds (`s`) | Executor SDK | Go SDK (`dbos/conductor.go:29`), Python SDK (`conductor.py:55`), TS SDK (`conductor.ts:29`), Java SDK (`Conductor.java:89`) |
| Initial reconnect delay | 1 second | seconds (`s`) | Executor SDK | Go SDK (`dbos/conductor.go:30`), TS SDK (`conductor.ts:31`), Java SDK (`Conductor.java:90`) |
| Maximum reconnect delay | 30 seconds | seconds (`s`) | Executor SDK | Go SDK (`dbos/conductor.go:31`) |
| Handshake timeout | 5s (TS) / 10s (Go) | seconds (`s`) | Executor SDK | Go SDK (`dbos/conductor.go:32`), TS SDK (`conductor.ts:112`) |
| Write deadline | 5 seconds | seconds (`s`) | Executor SDK | Go SDK (`dbos/conductor.go:33`) |
| Executor timeout grace period | 60 seconds | seconds (`s`) | Control plane (Relay) | DBOS docs (`/production/workflow-recovery`), OpenAPI `executorTimeoutSecs` |
| Default GC batch size | 10,000 rows | count | SDK / Control plane | Go SDK (`conductor.go:34`), Python SDK (`_sys_db.py:DEFAULT_GC_BATCH_SIZE`) |

### Heartbeat and liveness protocol

Liveness detection operates as follows:
1. **Heartbeat initiation**: The executor SDK actively initiates heartbeats by
   sending a standard WebSocket `Ping` frame (opcode 0x9) every 20 seconds
   (`_PING_INTERVAL` / `pingPeriodMs`). In parallel, Relay sends periodic WebSocket
   `Ping` frames at the matching 20-second interval (`internal/hub/conn.go`).
2. **Server expectation**: The server maintains a read deadline or liveness
   timer of 25 seconds (`executorPingWait`). Receipt of any WebSocket frame
   (Ping or Text) resets this timer and refreshes the executor's `updatedAt`
   timestamp.
3. **Missed heartbeat detection**: If no frame arrives within 25 seconds, or if
   the underlying TCP/WebSocket connection closes abnormally, the server declares
   the connection dropped and immediately transitions the executor to
   `DISCONNECTED`.
4. **Client-side reconnect**: If the executor does not receive a `Pong` frame
   within its pong timeout (15s in Python/TS/Java, 30s in Go), the executor
   terminates the socket and initiates a reconnect with an initial 1s delay and
   jittered exponential backoff capped at 30s.

## Executor lifecycle state machine

The Conductor OpenAPI specification (`components.schemas.Executor.properties.status`)
defines three lifecycle statuses: `HEALTHY`, `DISCONNECTED`, and `DEAD`.

```
                    +-----------------------+
                    |        HEALTHY        |<-----------------------+
                    +-----------------------+                        |
                                |                                    |
                Socket closed / |                                    |
               Ping wait > 25s  |                                    |
                                v                                    |
                    +-----------------------+                        |
                    |     DISCONNECTED      |                        |
                    +-----------------------+                        |
                                |                                    |
        Grace period elapses    |        Reconnect within            |
       (executorTimeoutSecs,    |       executorTimeoutSecs          |
            default 60s)        |        with same executor_id       |
                                v                                    |
                    +-----------------------+                        |
                    |         DEAD          |                        |
                    +-----------------------+                        |
                                |                                    |
                     Dispatch recovery to                            |
                     healthy peer executor                           |
                                |                                    |
                     Recovery acknowledged                           |
                     with { success: true }                          |
                                |                                    |
                                v                                    |
                     Delete dead executor                            |
                      registration record                            |
```

### State transition rules

1. **Registration into `HEALTHY`**:
   Upon establishing the WebSocket connection, the control plane sends an
   `executor_info` request. The executor replies with its registration metadata
   (`executor_id`, `application_version`, `hostname`, `language`, `dbos_version`,
   `executor_metadata`). Relay inserts or updates the record in its store with
   status `HEALTHY` and sets `createdAt` and `updatedAt` to the current timestamp.

2. **`HEALTHY` to `DISCONNECTED`**:
   When the WebSocket connection closes (cleanly or abnormally) or when 25
   seconds elapse without a ping frame, Relay marks the executor `DISCONNECTED`
   and starts a grace period timer configured by `executorTimeoutSecs` (default
   60 seconds).

3. **Reconnection while `DISCONNECTED`**:
   If an executor reconnects presenting the same `executor_id` before the grace
   period expires:
   * The pending dead timer is cancelled.
   * The executor status reverts to `HEALTHY`.
   * No workflow recovery is triggered. Workflows continue running on that
     executor without interruption.

4. **`DISCONNECTED` to `DEAD`**:
   If the grace period elapses without the executor reconnecting:
   * The executor status transitions to `DEAD`.
   * The recovery engine selects a candidate healthy executor belonging to the
     same application.
   * The recovery engine dispatches a recovery message over the candidate's
     open WebSocket.

5. **Final deletion after recovery**:
   Per upstream documentation: "After recovery is confirmed, Conductor deletes
   its record of the executor." Relay removes the dead executor from its
   `executors` table only after receiving a successful recovery response from the
   assigned healthy executor.

## Per-application configuration overrides

Application-level overrides are defined by the `Application` and
`PatchAppInputBody` schemas in `api/spec/openapi.json` and manipulated via
`PATCH /v2/orgs/{orgName}/apps/{appName}` (`updateApp`).

| Property | OpenAPI Schema Type | Unit | Default | Description |
| --- | --- | --- | --- | --- |
| `executorTimeoutSecs` | `integer` (int64) | **Seconds** (`s`) | `60` | Grace period after disconnection before executor is marked `DEAD` and recovery starts. |
| `gcTimeThresholdMs` | `integer`, nullable | **Milliseconds** (`ms`) | `null` (disabled) | Age threshold for completed workflow history. Completed workflows older than this duration are deleted. |
| `gcRowsThreshold` | `integer`, nullable | **Row count** | `null` (disabled) | Maximum completed workflows retained. Excess older rows are purged. |
| `globalTimeoutMs` | `integer`, nullable | **Milliseconds** (`ms`) | `null` (disabled) | Maximum workflow run duration. Unfinished workflows older than this duration from start/enqueue are cancelled. |
| `privateMode` | `boolean` | Flag | `false` | When true, disables telemetry and payload metadata retention. |

### Unit discipline: seconds versus milliseconds

The unit conventions in the Conductor REST specification are heterogeneous:
* `executorTimeoutSecs` is expressed in **seconds**.
* `gcTimeThresholdMs` and `globalTimeoutMs` are expressed in **milliseconds**.
* The public DBOS Console and documentation describe retention thresholds and
  global timeouts in **hours**, but the underlying HTTP REST contract exchanges
  raw integer milliseconds.

Conflating seconds and milliseconds represents a 1000x timing discrepancy.
If Relay interpreted `executorTimeoutSecs` as milliseconds, an executor would be
declared dead 60 milliseconds after socket drop, causing spurious workflow
recovery while an executor was merely performing a fast restart. Conversely,
interpreting `globalTimeoutMs` as seconds would extend workflow execution
deadlines by a factor of 1,000.

Relay's internal data model and configuration schemas must store durations with
explicit time unit types (`time.Duration` in Go) and explicitly map to and from
the exact units defined in the OpenAPI schema during JSON serialization.

## Recovery protocol wire semantics

When an executor is declared `DEAD`, Relay initiates recovery by sending a
message over WebSocket to a chosen healthy executor of the same application.

### Wire message format

The recovery request dispatched to the healthy executor is:

```json
{
  "type": "recovery",
  "request_id": "0191e4f2-9d3c-789a-bcde-f0123456789a",
  "executor_ids": ["executor-dead-uuid"]
}
```

The healthy executor processes the request and replies with:

```json
{
  "type": "recovery",
  "request_id": "0191e4f2-9d3c-789a-bcde-f0123456789a",
  "success": true,
  "error_message": null
}
```

### SDK recovery execution

Upon receiving the recovery message, the recovering executor interacts with the
shared system database:
* Go SDK (`dbos/recovery.go:11`): executes `sysdb.ReenqueueForRecovery` with the
  dead executor IDs, the recovering executor's application version, and the
  internal queue name (`models.InternalQueueName`).
* Python SDK (`dbos/_conductor/conductor.py:152`): executes
  `self.dbos._recover_pending_workflows(recovery_message.executor_ids)`.
* TypeScript SDK (`src/conductor/conductor.ts:154`): executes
  `await this.dbosExec.recoverPendingWorkflows(recoveryMsg.executor_ids)`.

The database update atomically resets the owner and status of all pending
workflows that belonged to the dead executor, re-enqueuing them into the local
task queue for execution.

Because DBOS workflows follow an at-least-once execution model for steps and
exactly-once guarantee for outcomes, repeated recovery dispatch is safe and
idempotent.

## Impact on recovery engine

Relay implements the recovery engine internally. The findings in this
document impose specific requirements on its architecture:

1. **Connection hub liveness tracking**:
   * Relay must maintain a per-connection timer reset on any incoming frame.
   * If no frame is received within 25 seconds (`executorPingWait`), Relay must
     transition the executor to `DISCONNECTED` and start the grace period timer.
   * If a WebSocket connection closes, Relay must transition the executor to
     `DISCONNECTED` immediately without waiting for the 25-second ping wait.

2. **Grace period timer management**:
   * Each disconnected executor must have a dedicated timer set to
     `executorTimeoutSecs` (retrieved from the application's configuration,
     defaulting to 60 seconds if unset).
   * The timer must be cancelled if a connection is established with the same
     `executor_id` for that application.
   * If Relay runs in a multi-instance cluster, timer ownership must be tied to
     the instance holding the connection lease, or coordinated via a shared
     distributed lease store.

3. **Candidate executor selection algorithm**:
   When an executor becomes `DEAD`, the recovery dispatcher must pick an executor
   satisfying:
   * Same organisation (`orgName`) and application (`appName`).
   * Current status is `HEALTHY`.
   * Version preference: Prefer an executor running the same
     `application_version` as the dead executor. If none exists, failover to an
     executor running another version only if allowed by application settings,
     or surface an alert for operator intervention.

4. **Confirmation and deletion order**:
   * Relay must not remove the dead executor record upon entering `DEAD`.
   * Relay dispatches `recovery` with `executor_ids: [dead_id]` to the chosen
     healthy executor.
   * Upon receiving a response matching `request_id` with `success: true`, Relay
     deletes the dead executor record from its database.
   * If the recovering executor fails, times out, or disconnects before
     replying, Relay selects another healthy executor and retries with backoff.

5. **Retention and global timeout scheduler**:
   * Relay must periodically inspect configured applications.
   * If `globalTimeoutMs` is configured, Relay computes
     `timeout_cutoff_epoch_ms = now_epoch_ms - globalTimeoutMs`.
   * If `gcTimeThresholdMs` is configured, Relay computes
     `gc_cutoff_epoch_ms = now_epoch_ms - gcTimeThresholdMs`.
   * Relay dispatches a `retention` wire message to a healthy executor with
     payload:
     ```json
     {
       "type": "retention",
       "request_id": "<uuid>",
       "body": {
         "gc_cutoff_epoch_ms": 1725753600000,
         "gc_rows_threshold": 50000,
         "gc_batch_size": 10000,
         "timeout_cutoff_epoch_ms": 1725750000000
       }
     }
     ```
   * The healthy executor executes the database maintenance queries asynchronously
     off its main message loop, preventing retention workload from blocking
     liveness frames.

6. **Audit logging**:
   * Every state transition (`HEALTHY` -> `DISCONNECTED`, `DISCONNECTED` ->
     `HEALTHY`, `DISCONNECTED` -> `DEAD`), recovery dispatch, and executor
     record deletion must be emitted to Relay's audit log.
