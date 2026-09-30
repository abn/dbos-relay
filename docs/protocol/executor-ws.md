---
title: Conductor Protocol (WebSocket)
type: Reference
---
# Conductor Protocol (WebSocket)

The Executor WebSocket protocol defines full-duplex communication between Relay and registered DBOS Conductor executors.

## Connection

The Conductor WebSocket URL is constructed by stripping any trailing slashes from the base URL and appending `/websocket/{appName}/{conductorKey}`:

```text
wss://<relay-host>/websocket/{appName}/{conductorKey}
```

The API key is transmitted in the URL path (`{conductorKey}`).

Relay derives this protocol from the following public DBOS Transact SDK repositories:
* Python SDK: `dbos-transact-py` (commit `2b93e1467a5464f817ef5d11aa5f10d3d2253761`)
* TypeScript SDK: `dbos-transact-ts` (commit `749a4d420127e97715bf1f5d8caabf496a40af0b`)
* Go SDK: `dbos-transact-golang` (commit `fb3e33e0b4c3c709b9271eb935adce5eaf9386f5`)
* Java SDK: `dbos-transact-java` (commit `ecc2bda4deb57e3ba38c55cca150e95c99eb9d64`)

### Handshake Sequence

Upon WebSocket connection establishment, Relay initiates the handshake by sending an `executor_info` request frame to the executor (`conductor_protocol.go:82-95`):

```json
{
  "type": "executor_info",
  "request_id": "req-init-1"
}
```

The connecting executor must reply with an `executor_info` response frame containing registration metadata:

```json
{
  "type": "executor_info",
  "request_id": "req-init-1",
  "executor_id": "exec-uuid",
  "application_version": "1.0.0",
  "hostname": "worker-1",
  "language": "go",
  "dbos_version": "0.1.0",
  "executor_metadata": {}
}
```

* `executor_id`: Unique identifier for the executor instance.
* `application_version`: Application release version deployed on the executor.
* `hostname`: Host running the executor process.
* `language`: Runtime language (`go`, `typescript`, `python`, `java`).
* `dbos_version`: SDK version string.
* `executor_metadata`: Optional key-value metadata map.

## Envelope

All wire messages share a common JSON envelope (`internal/protocol/envelope.go:41-46`):

* `type` (string): The message type discriminator.
* `request_id` (string): Unique identifier correlating requests with responses.
* `error_message` (string, optional): Present only in response messages when an operation fails.

> [!NOTE]
> `error` and `payload` are not valid wire envelope fields.

## Message Catalogue

Relay supports 33 distinct protocol message types, matching the upstream Go SDK (`dbos-transact-golang/dbos/conductor_protocol.go` commit `fb3e33e0b4c3c709b9271eb935adce5eaf9386f5`).

### Core Lifecycle and Execution

1. `executor_info`
   * Request: Envelope only.
   * Response: `executor_id` (string), `application_version` (string), `hostname` (string, optional), `language` (string), `dbos_version` (string), `executor_metadata` (object, optional).

2. `recovery`
   * Request: `executor_ids` (array of strings). Dispatched to instruct an active executor to recover workflows previously assigned to dead executor instances.
   * Response: `success` (boolean).

3. `cancel`
   * Request: `workflow_id` (string, optional), `workflow_ids` (array of strings, optional), `cancel_children` (boolean).
   * Response: `success` (boolean).

4. `resume`
   * Request: `workflow_id` (string, optional), `workflow_ids` (array of strings, optional), `queue_name` (string, optional).
   * Response: `success` (boolean).

5. `delete`
   * Request: `workflow_id` (string, optional), `workflow_ids` (array of strings, optional), `delete_children` (boolean).
   * Response: `success` (boolean).

6. `exist_pending_workflows`
   * Request: `executor_id` (string), `application_version` (string).
   * Response: `exist` (boolean).

7. `retention`
   * Request: `body` (object): `gc_cutoff_epoch_ms` (integer, optional), `gc_rows_threshold` (integer, optional), `gc_batch_size` (integer, optional), `timeout_cutoff_epoch_ms` (integer, optional).
   * Response: `success` (boolean).

### Workflow Inspection and Queries

8. `list_workflows`
   * Request body: `workflow_uuids` (array of strings, optional), `workflow_name` (string or array, optional), `authenticated_user` (string or array, optional), `start_time` (timestamp, optional), `end_time` (timestamp, optional), `status` (string or array, optional), `application_version` (string or array, optional), `limit` (integer, optional), `offset` (integer, optional), `sort_desc` (boolean), `load_input` (boolean), `load_output` (boolean), `queues_only` (boolean).
   * Response: `output` (array of workflow status records).

9. `list_queued_workflows`
   * Request body: Same query fields as `list_workflows`, with `queue_name` filter.
   * Response: `output` (array of workflow status records).

10. `get_workflow`
    * Request: `workflow_id` (string), `load_input` (boolean), `load_output` (boolean).
    * Response: `output` (single workflow status record).

11. `list_steps`
    * Request: `workflow_id` (string), `load_output` (boolean), `limit` (integer, optional), `offset` (integer, optional).
    * Response: `output` (array of step records).

12. `get_workflow_events`
    * Request: `workflow_id` (string).
    * Response: `events` (array of `{key, value}`).

13. `get_workflow_notifications`
    * Request: `workflow_id` (string).
    * Response: `notifications` (array of `{topic, message, created_at_epoch_ms, consumed}`).

14. `get_workflow_streams`
    * Request: `workflow_id` (string).
    * Response: `streams` (array of `{key, values}`).

15. `get_workflow_aggregates`
    * Request body: Aggregation filter parameters.
    * Response: `output` (array of aggregate rows).

16. `get_step_aggregates`
    * Request body: Step aggregation filter parameters.
    * Response: `output` (array of step aggregate rows).

### Workflow Forking and Portability

17. `fork_workflow`
    * Request body: `workflow_id` (string), `start_step` (integer), `new_workflow_id` (string, optional), `application_version` (string, optional), `queue_name` (string, optional), `queue_partition_key` (string, optional).
    * Response: `new_workflow_id` (string, optional).

18. `rewind_workflow`
    * Request body: `workflow_id` (string), `start_step` (integer, optional), `application_version` (string, optional), `queue_name` (string, optional), `queue_partition_key` (string, optional). Replays a workflow from an earlier step and discards the history after it; the workflow keeps its identifier, unlike a fork.
    * Response: `success` (boolean).

19. `fork_from_failure`
    * Request body: `workflow_ids` (array of strings), `application_version` (string, optional), `queue_name` (string, optional), `queue_partition_key` (string, optional), `from_last_failure` (boolean, optional), `from_last_step` (boolean, optional), `from_step` (integer, optional), `from_step_name` (string, optional).
    * Response: `forked_workflow_ids` (array of strings).

20. `export_workflow`
    * Request: `workflow_id` (string), `export_children` (boolean).
    * Response: `serialized_workflow` (string, optional).

21. `import_workflow`
    * Request: `serialized_workflow` (string).
    * Response: `success` (boolean).

### Schedules

22. `list_schedules`
    * Request: `body` (object, optional filter parameters: `status`, `workflow_name`, `schedule_name_prefix`, `application_name`, `load_context`).
    * Response: `output` (array of schedule objects).

23. `get_schedule`
    * Request: `schedule_name` (string), `load_context` (boolean, optional).
    * Response: `output` (schedule object).

24. `pause_schedule`
    * Request: `schedule_name` (string).
    * Response: `success` (boolean).

25. `resume_schedule`
    * Request: `schedule_name` (string).
    * Response: `success` (boolean).

26. `trigger_schedule`
    * Request: `schedule_name` (string).
    * Response: `workflow_id` (string, optional).

27. `backfill_schedule`
    * Request: `schedule_name` (string), `start` (timestamp string, ISO 8601), `end` (timestamp string, ISO 8601).
    * Response: `workflow_ids` (array of strings).

### Queues and Metrics

28. `list_queues`
    * Request: `body` (object, optional filter parameter `application_name`).
    * Response: `output` (array of queue metadata objects).

29. `get_queue`
    * Request: `name` (string).
    * Response: `output` (queue metadata object).

30. `get_metrics`
    * Request: `start_time` (timestamp string, RFC 3339), `end_time` (timestamp string, RFC 3339), `metric_class` (string), `application_name` (array of strings, optional).
    * Response: `metrics` (array of `{metric_name, metric_type, value}`).

### Applications and Alerts

31. `list_application_versions`
    * Request: Envelope only.
    * Response: `output` (array of application version objects with `{version_id, version_name, version_timestamp, created_at}`).

32. `set_latest_application_version`
    * Request: `version_name` (string).
    * Response: `success` (boolean).

33. `alert`
    * Request: Dispatched by Relay to connected executors with `{name, message, metadata}` (`conductor_protocol.go:590-595`).
    * Response: `success` (boolean).

### Unimplemented Types

* `restart`: Present in Python (`protocol.py:37`) and Java SDKs, but not implemented in Relay or the Go SDK.

## Liveness, Heartbeat, and Timeout

Relay and connected executors exchange heartbeats to maintain active connection health:

* Server ping interval: 10 seconds.
* Client ping interval: 10 seconds (or SDK default of 20 seconds).
* Pong deadline: 25 seconds (`executorPingWait`).
* Reconnect backoff: Executors apply exponential backoff between 1 second and SDK-defined ceilings on disconnect.

## Recovery Semantics

Relay monitors executor connectivity and orchestrates workflow recovery across peer instances.

### Lifecycle State Machine

Each executor registration transitions through four discrete states:

1. **Connected (`HEALTHY`)**: Active WebSocket connection responding to heartbeat frames.
2. **Disconnected (`DISCONNECTED`)**: Socket connection dropped or timed out. A per-application grace timer begins. If the executor reconnects with matching `executor_id` before expiry, it returns to `HEALTHY`.
3. **Dead (`DEAD`)**: The grace period expired without reconnection. Relay marks the instance dead and initiates workflow recovery dispatch.
4. **Deleted**: Once recovery is acknowledged by an active peer, the dead executor record is pruned from the registry.

### Timing and Grace Periods

* Default grace period: 60 seconds (cited from DBOS public documentation `/production/workflow-recovery`).
* Application override: Configurable per-application via the `executorTimeoutSecs` setting in application metadata (Conductor OpenAPI `Application` and `PatchAppInputBody`).
* Server ping interval: 10 seconds.

### Recovery Failover and Peer Selection

When an executor transitions to `DEAD`:

1. Relay queries connected healthy peers within the same application.
2. Cross-application or cross-organisation recovery is rejected.
3. Candidates with a matching `application_version` are prioritized to prevent workflow version skew.
4. Relay dispatches a `recovery` message with `executor_ids: [dead_executor_id]` to the selected peer.
5. If the chosen peer disconnects or fails to acknowledge within the dispatch deadline, Relay fails over sequentially to the next healthy candidate.
6. Upon receiving a response with `success: true`, Relay deletes the dead executor record from the registry.
7. If no healthy peers are currently connected, the dead record remains in `DEAD` status until a peer connects.

### Idempotence Guarantees

Relay inherits the execution guarantees of the upstream SDK implementation and system database:

* Step executions are at-least-once.
* Workflow outcomes are exactly-once.
* Recovery dispatch is idempotent. Multiple recovery dispatches for the same dead executor do not corrupt execution state because recovery re-enqueue in the SDK system database (`dbos-transact-go` `dbos/internal/sysdb/system_database.go:5098` `ReenqueueForRecovery`) is scoped to `status = PENDING`. Rows previously transitioned to `ENQUEUED` by an initial dispatch are unaffected by duplicate dispatches.

## Alert Notifications

Relay evaluates alerting rules against application metrics and dispatches an `alert` frame to registered executors:

```json
{
  "type": "alert",
  "request_id": "alert-uuid",
  "name": "high_cpu_utilization",
  "message": "CPU usage exceeded threshold",
  "metadata": {
    "metric_name": "cpu_utilization",
    "threshold": "90"
  }
}
```

Receiving executors dispatch alerts to registered handlers (`@DBOS.alert_handler` in Python, `DBOS.setAlertHandler` in TypeScript, and conductor protocol handler in Go).

## High Availability and Peer Forwarding

In multi-instance deployments, Relay instances coordinate state through the shared control plane database:

1. **Instance Registration**: Each Relay instance registers its unique ID, advertise address, and port in the `instances` table and maintains a periodic heartbeat.
2. **Executor Lease Ownership**: Executors connecting via WebSocket are assigned to the receiving Relay instance with a lease. Surviving instances adopt expired leases upon heartbeat timeout.
3. **Cross-Instance Peer Forwarding**: When an API request targets an application whose connected executors reside on a peer Relay instance, the receiving instance signs the payload using HMAC-SHA256 and forwards it over HTTP to `/internal/v1/forward/{appID}` on the target peer.
4. **Loop Prevention and Drift Check**: Forwarded requests carry an `X-Relay-Forward-Hop` header. Requests with `hop >= 1` are rejected with HTTP 409 Conflict to prevent forwarding loops. Forward signatures include Unix timestamps with a maximum allowed drift of 30 seconds.

## SDK Differences Matrix

| Feature | TypeScript | Python | Go | Java |
|---|---|---|---|---|
| `metadata` | Supported | Supported | Supported | Supported |
| `cancel_children` | Supported | Supported | Supported (`conductor_protocol.go:462-467`) | Supported (`CancelRequest.java:8`) |
| `rewind_workflow` | Supported | Supported | Supported (`conductor_protocol.go`) | Not yet implemented |

## Version Skew Policy

* SDK clients log unknown message types and reply with structured error responses (`dbos-transact-go/dbos/conductor.go:433`, `dbos-transact-ts/src/conductor/conductor.ts:921`, `Conductor.java:255`, `conductor.py:1168`).
* Standard JSON unmarshalling in SDKs ignores unknown fields on typed unmarshal.
