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
* Python SDK: `dbos-transact-py` (commit `833794f7a1138bacf75ff6d88647a33eb5e35e52`)
* TypeScript SDK: `dbos-transact-ts` (commit `d8c4974cca6cc84b296f3b8edfbbb41627ddd47e`)
* Go SDK: `dbos-transact-go` (commit `ab56911fdd78552e1e7fe648cff7c831a1e760c8`)
* Java SDK: `dbos-transact-java` (commit `1248174f393bd97f9973ec83cbc6e42b6e319ed1`)

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

Relay supports 32 distinct protocol message types, matching the upstream Go SDK (`dbos-transact-go/dbos/conductor_protocol.go` commit `ab56911fdd78552e1e7fe648cff7c831a1e760c8`).

### Core Lifecycle and Execution

1. `executor_info`
   * Request: Envelope only.
   * Response: `executor_id` (string), `application_version` (string), `hostname` (string, optional), `language` (string), `dbos_version` (string), `executor_metadata` (object, optional).

2. `recovery`
   * Request: `executor_ids` (array of strings). Dispatched to instruct an active executor to recover workflows previously assigned to dead executor instances.
   * Response: `success` (boolean).

3. `cancel`
   * Request: `workflow_id` (string), `cancel_children` (boolean).
   * Response: `success` (boolean).

4. `resume`
   * Request: `workflow_id` (string).
   * Response: `success` (boolean).

5. `delete`
   * Request: `workflow_ids` (array of strings).
   * Response: `success` (boolean).

6. `exist_pending_workflows`
   * Request: Envelope only.
   * Response: `exist` (boolean).

7. `retention`
   * Request: Envelope only.
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
    * Request: `workflow_id` (string).
    * Response: `output` (array of step records).

12. `get_workflow_events`
    * Request: `workflow_id` (string).
    * Response: `output` (map of event key to serialized event data).

13. `get_workflow_notifications`
    * Request: `workflow_id` (string).
    * Response: `output` (array of notification records).

14. `get_workflow_streams`
    * Request: `workflow_id` (string).
    * Response: `output` (map of stream key to string array).

15. `get_workflow_aggregates`
    * Request body: Aggregation filter parameters.
    * Response: `output` (array of aggregate rows).

16. `get_step_aggregates`
    * Request body: Step aggregation filter parameters.
    * Response: `output` (array of step aggregate rows).

### Workflow Forking and Portability

17. `fork_workflow`
    * Request body: `workflow_id` (string), `start_step` (integer), `new_workflow_id` (string, optional), `application_version` (string, optional), `queue_name` (string, optional), `queue_partition_key` (string, optional).
    * Response: `workflow_id` (string).

18. `fork_from_failure`
    * Request body: `workflow_id` (string), `new_workflow_id` (string, optional), `application_version` (string, optional).
    * Response: `forked_workflow_ids` (array of strings).

19. `export_workflow`
    * Request: `workflow_id` (string), `export_children` (boolean).
    * Response: `output` (serialized workflow execution structure).

20. `import_workflow`
    * Request: `serialized_workflow` (string).
    * Response: `success` (boolean).

### Schedules

21. `list_schedules`
    * Request: Envelope only.
    * Response: `output` (array of schedule objects).

22. `get_schedule`
    * Request: `schedule_id` (string).
    * Response: `output` (schedule object).

23. `pause_schedule`
    * Request: `schedule_id` (string).
    * Response: `success` (boolean).

24. `resume_schedule`
    * Request: `schedule_id` (string).
    * Response: `success` (boolean).

25. `trigger_schedule`
    * Request: `schedule_id` (string).
    * Response: `workflow_id` (string).

26. `backfill_schedule`
    * Request: `schedule_id` (string), `start_time` (timestamp), `end_time` (timestamp).
    * Response: `workflow_ids` (array of strings).

### Queues and Metrics

27. `list_queues`
    * Request: Envelope only.
    * Response: `output` (array of queue metadata objects).

28. `get_queue`
    * Request: `queue_name` (string).
    * Response: `output` (queue metadata object).

29. `get_metrics`
    * Request: `start_time` (timestamp, optional), `end_time` (timestamp, optional).
    * Response: `metrics` (array of `{metric_name, metric_type, value}`).

### Applications and Alerts

30. `list_application_versions`
    * Request: Envelope only.
    * Response: `output` (array of application version strings).

31. `set_latest_application_version`
    * Request: `application_version` (string).
    * Response: `success` (boolean).

32. `alert`
    * Request: Dispatched by Relay to connected executors with `{name, message, metadata}` (`conductor_protocol.go:590-595`).
    * Response: None required (unidirectional notification).

### Unimplemented Types

* `restart`: Present in Python (`protocol.py:37`) and Java SDKs, but not implemented in Relay or the Go SDK.

## Liveness and Timeout

Relay and connected executors exchange heartbeats to maintain active connection health:

* Server ping interval: 20 seconds (`_PING_INTERVAL` in `internal/hub/conn.go`).
* Client ping interval: 20 seconds default across SDKs.
* Pong timeout: 15 seconds.
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
* Server ping interval: 20 seconds.

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

## Version Skew Policy

* SDK clients log unknown message types and reply with structured error responses (`dbos-transact-go/dbos/conductor.go:433`, `dbos-transact-ts/src/conductor/conductor.ts:921`, `Conductor.java:255`, `conductor.py:1168`).
* Standard JSON unmarshalling in SDKs ignores unknown fields on typed unmarshal.
