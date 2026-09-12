---
title: Conductor Protocol (WebSocket)
type: Reference
---
# Conductor Protocol (WebSocket)

The Executor WebSocket protocol defines the communication between the Relay router and registered Conductor executors.

## Envelope

All messages share a common JSON envelope:
- `type`: The message type string.
- `request_id`: A unique identifier for the request, used to correlate responses.
- `error_message`: (Optional) Present only if the message is a response and an error occurred.

> [!NOTE]
> `error` and `payload` fields are not part of the standard envelope.

## Idempotence

Relay inherits the exact idempotence guarantees of the upstream SDK implementation. All retries and duplicates are handled according to the official semantics.

## Implemented Message Types

The following 32 message types are fully supported:

- `executor_info`: `{"type": "executor_info", "request_id": "req-1", "application_version": "1.0.0", "executor_metadata": {}}`
- `recovery`
- `cancel`
- `resume`
- `list_workflows`
- `list_steps`
- `get_workflow`
- `fork_workflow`
- `fork_from_failure`
- `exist_pending_workflows`
- `retention`
- `get_metrics`
- `export_workflow`
- `import_workflow`
- `delete`
- `alert`: `{"type": "alert", "request_id": "req-1", "name": "alert1"}`
- `list_schedules`
- `get_schedule`
- `pause_schedule`
- `resume_schedule`
- `backfill_schedule`
- `trigger_schedule`
- `get_workflow_events`
- `get_workflow_notifications`
- `get_workflow_streams`
- `get_workflow_aggregates`
- `get_step_aggregates`
- `list_application_versions`
- `set_latest_application_version`
- `list_queues`
- `get_queue`
- `list_queued_workflows`

## Unimplemented

- `restart`: Currently unimplemented.
