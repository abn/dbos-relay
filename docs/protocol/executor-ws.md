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

When `recovery` is dispatched, the SDK retrieves the previous executor IDs and replays workflows.
Idempotence guarantees:
* Steps are at-least-once.
* Outcomes are exactly-once.

## SDK Differences Matrix

| Field | TypeScript | Python | Go | Java |
|---|---|---|---|---|
| `metadata` | Yes | Yes | Yes | Yes |
| `cancel_children` | Yes | Yes | No | No |

## Version Skew Policy

The conductor expects SDKs to match the current protocol version, logging warnings for unrecognized fields.
