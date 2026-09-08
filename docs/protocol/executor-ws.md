---
title: Executor WebSocket Protocol
type: Reference
---
# Executor WebSocket Protocol

## Connection

The Conductor WebSocket URL is constructed by removing any trailing slashes from the base URL and appending `/websocket/{appName}/{conductorKey}`.
* Python SDK (`dbos-transact-py`, commit `833794f7a1138bacf75ff6d88647a33eb5e35e52`)
* TypeScript SDK (`dbos-transact-ts`, commit `d8c4974cca6cc84b296f3b8edfbbb41627ddd47e`)
* Go SDK (`dbos-transact-go`, commit `ab56911fdd78552e1e7fe648cff7c831a1e760c8`)
* Java SDK (`dbos-transact-java`, commit `1248174f393bd97f9973ec83cbc6e42b6e319ed1`)

The API key is transmitted in the URL path (`{conductorKey}`).

## Registration / Executor Info

The `executor_info` message is the first sent after connection. The `executor_id` is typically derived from `DBOS__VMID` if present, else a generated UUID/hostname combination.

```json
{
  "type": "executor_info",
  "request_id": "req-uuid",
  "executor_id": "...",
  "app_version": "...",
  "hostname": "...",
  "language": "typescript|python|go|java",
  "dbos_version": "...",
  "metadata": {}
}
```

## Message Envelope

Common fields across all messages:
* `type` (string): The message type discriminator.
* `request_id` (string): A unique identifier for the request.
* `error` (string, optional): An error message, if any.
* `payload` (object, optional): Additional data.

JSON fields use `snake_case`.

## Complete Message Catalogue

* `executor_info`: Connect handshake and executor registration.
* `recovery`: Recover workflows across executor restart. Payload: `executor_ids`.
* `list_workflows`: Fetch workflow status.
* `list_queued_workflows`: Fetch queued workflows.
* `get_workflow`: Get a single workflow by ID.
* `list_steps`: List step information.
* `cancel`: Cancel a workflow execution.
* `resume`: Resume a suspended workflow.
* `delete`: Delete workflow history.
* `restart`: Restart a failed workflow.
* `exist_pending_workflows`: Check if workflows are pending.
* Events, notifications, streams, retention: Addressed via SDK-specific extensions.

## Liveness & Timeout

* Ping interval: 20s (TypeScript), varies slightly by SDK.
* Ping timeout: 15s.
* Reconnect backoff: Delay scaling from 1s up to maximum thresholds on repeated disconnects.

## Recovery Semantics

Relay monitors executor connectivity and coordinates workflow recovery across peer instances.

### Lifecycle State Machine

Each executor registration transitions through four discrete states:

1. **Connected (`HEALTHY`)**: The executor maintains an active WebSocket connection and responds to heartbeats.
2. **Disconnected (`DISCONNECTED`)**: The WebSocket connection closed or timed out. Relay starts a per-application grace period timer. If the executor reconnects with the same `executor_id` before the timer expires, it returns to `Connected` and the timer is cancelled.
3. **Dead (`DEAD`)**: The grace period timer expired without reconnection. Relay marks the executor dead and triggers workflow recovery dispatch.
4. **Deleted**: Following successful recovery acknowledgment by a healthy peer, the dead executor record is removed from the registry.

### Timing and Grace Periods

* Default grace period: 60 seconds (cited from DBOS public documentation `/production/workflow-recovery`).
* Per-application override: Configurable via the `executorTimeoutSecs` key in application settings (Conductor OpenAPI `Application` and `PatchAppInputBody`).
* Server ping interval: 20 seconds.
* Read/pong deadline: 25 seconds.

### Recovery Failover and Peer Selection

When an executor transitions to `Dead`:

1. Relay queries connected peers within the same application.
2. Cross-application or cross-organisation recovery is strictly rejected.
3. If multiple peers exist, candidates with a matching `application_version` are prioritized to avoid workflow version skew.
4. Relay dispatches a `recovery` message containing `executor_ids: [dead_executor_id]`.
5. If the chosen peer disconnects or fails to respond within the dispatch deadline, Relay fails over sequentially to the next healthy candidate.
6. Upon receiving a response with `success: true`, Relay deletes the dead executor record from the control plane database.
7. If no healthy peers are currently connected, the dead executor record remains in `DEAD` status until a new peer connects.

### Idempotence Guarantees

* Step executions are at-least-once.
* Workflow outcomes are exactly-once.
* Recovery dispatch is idempotent; multiple dispatches for the same dead executor do not corrupt execution state.

## SDK Differences Matrix

| Field | TypeScript | Python | Go | Java |
|---|---|---|---|---|
| `metadata` | Yes | Yes | Yes | Yes |
| `cancel_children` | Yes | Yes | No | No |

## Version Skew Policy

The conductor expects SDKs to match the current protocol version, logging warnings for unrecognized fields.
