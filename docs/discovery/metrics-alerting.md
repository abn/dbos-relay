---
type: Reference
title: Metrics, alerting, and HA peer forwarding
description: OpenMetrics and Prometheus metrics catalogue, alerting schemas, and HA peer forwarding conventions.
status: draft
---

# Metrics, alerting, and HA peer forwarding

Relay provides observability and operational alerting compatible with DBOS Conductor.
This specification details the OpenMetrics scrape surface, the Conductor REST metrics
contract, the alerting rule data model, the WebSocket alert delivery protocol, and the
high-availability peer forwarding conventions.

## Provenance and clean-room citations

All specifications in this document are derived from permitted public sources:

* **DBOS Public Documentation**:
  * Metrics reference: `https://docs.dbos.dev/production/metrics` (confirmed 2026-09-08)
  * Alerting guide: `https://docs.dbos.dev/production/alerting` (confirmed 2026-09-08)
  * Self-hosting Conductor guide: `https://docs.dbos.dev/production/hosting-conductor` (confirmed 2026-09-08)
* **Vendored Conductor OpenAPI Specification**:
  * Specification file: `api/spec/openapi.json` (OpenAPI 3.1.0, SHA256: `b5dc31eb29686a84fe0390a7446b5acdbc0dd05846cc94a746de649b92880722`)
  * Schemas: `Metric` (`#/components/schemas/Metric`), `AlertingRule` (`#/components/schemas/AlertingRule`), `CreateAlertInputBody` (`#/components/schemas/CreateAlertInputBody`)
  * Paths: `#/paths/~1v2~1orgs~1{orgName}~1apps~1{appName}~1metrics`, `#/paths/~1v2~1orgs~1{orgName}~1apps~1{appName}~1alerting-rules`, `#/paths/~1v2~1orgs~1{orgName}~1apps~1{appName}~1alerting-rules~1{ruleId}`
* **DBOS Transact SDKs**:
  * Go SDK (`https://github.com/dbos-inc/dbos-transact-go`, commit `ab56911fdd78552e1e7fe648cff7c831a1e760c8`):
    * `dbos/conductor_protocol.go`: `alertMessage` ("alert"), `getMetricsMessage` ("get_metrics"), `alertRequest`, `alertConductorResponse`, `getMetricsConductorRequest`, `getMetricsConductorResponse`
    * `dbos/conductor.go`: `handleAlertRequest` (lines 1229-1269)
    * `dbos/internal/sysdb/system_database.go`: `MetricData`, `GetMetrics` (lines 5449-5480)
  * Python SDK (`https://github.com/dbos-inc/dbos-transact-py`, commit `833794f7a1138bacf75ff6d88647a33eb5e35e52`):
    * Alert handler decorator: `@DBOS.alert_handler`
  * TypeScript SDK (`https://github.com/dbos-inc/dbos-transact-ts`, commit `d8c4974cca6cc84b296f3b8edfbbb41627ddd47e`):
    * Alert handler hook: `DBOS.setAlertHandler`

## OpenMetrics scrape surface

Conductor exposes application metrics via a single Prometheus-compatible HTTP endpoint.

### Endpoint specification

* **Path**: `/v1/metrics`
* **Method**: `GET`
* **Authentication**: Bearer token in the `Authorization` request header (`Authorization: Bearer <API_KEY>`). The key must carry the `application.read` or `metric.read` permission.
* **Content Negotiation**: `Accept: application/openmetrics-text` or Prometheus text exposition format.
* **Filtering Parameters** (repeatable query parameters):
  * `applications`: Limits emitted metrics to the specified application name(s).
  * `workflow_names`: Limits workflow-family metrics to the specified workflow name(s). Step families are labeled by `step_name`, a separate namespace, and are left unfiltered by this parameter.
  * `metrics`: Limits emitted metrics to the specified metric family names (e.g., `dbos_conductor_v1_workflow_success_rate`).
* **Label Stability**: `queue_name` is always present on queue-labeled families and may be empty for workflows that were never enqueued. Pending families carry no queue dimension.

### Exposition conventions

* **Metric Family Prefix**: All metric names use the prefix `dbos_conductor_v1_`.
* **Universal Label**: Every metric series carries an `application` label identifying the owning application.
* **OpenMetrics Type**: Every metric emitted by this endpoint is exposed as an OpenMetrics `gauge`.
* **Aggregation Window**: Scrapes return data for the most recently completed clock-aligned minute (`[T-1m, T)` where `T` is the current minute boundary).
* **Measurement Classes**:
  * **Rate**: Per-second average computed over the 60-second aggregation window. These values are pre-averaged gauges; scrapers must not apply the PromQL `rate()` function to them. Rate series carry the window end as their timestamp.
  * **Point-in-time**: Instantaneous snapshot value at scrape time, without an explicit timestamp.
  * **Windowed**: Aggregate values (such as maximum latency or queue wait) computed across events completed within the window, carrying the window end as their timestamp. Aggregations across multiple label groups must use `max()`.
  * **Timestamp Units**: Prometheus text exposition stamps timestamps in milliseconds; OpenMetrics stamps them in seconds.

### Metric catalogue

The tables below catalogue the OpenMetrics families defined by Conductor. Executor counts are evaluated directly from the control plane registry. Workflow and step families are computed per application by dispatching aggregate requests to a healthy executor over the existing WebSocket protocol for the most recently completed clock-aligned minute, with live executors taking precedence and series omitted when no executor answers.

#### In-scope metric families (Scale tier)

| Metric Name | Flavor | Labels | Origin | Status | Description |
| --- | --- | --- | --- | --- | --- |
| `dbos_conductor_v1_executor_count` | Point-in-time | `application`, `status`, `application_version` | Computed (Control Plane) | Supported | Number of registered executors. Series omitted when no executors are connected. `status` takes values such as `HEALTHY`, `DISCONNECTED`, or `DEAD`. |
| `dbos_conductor_v1_workflow_started_rate` | Rate | `application`, `workflow_name`, `queue_name` | Executor dispatch (`get_workflow_aggregates`, created window) | Supported | Workflows created per second over the 60-second window. |
| `dbos_conductor_v1_workflow_dequeued_rate` | Rate | `application`, `workflow_name`, `queue_name` | Executor dispatch (`get_workflow_aggregates`, dequeued window) | Supported | Enqueued workflows dequeued per second. Excludes workflows that were never enqueued. |
| `dbos_conductor_v1_workflow_success_rate` | Rate | `application`, `workflow_name`, `queue_name` | Executor dispatch (`get_workflow_aggregates`, completed window, `SUCCESS`) | Supported | Workflows that completed successfully per second. |
| `dbos_conductor_v1_workflow_failed_rate` | Rate | `application`, `workflow_name`, `queue_name` | Executor dispatch (`get_workflow_aggregates`, completed window, `ERROR` or `MAX_RECOVERY_ATTEMPTS_EXCEEDED`) | Supported | Workflows terminating in `ERROR` or `MAX_RECOVERY_ATTEMPTS_EXCEEDED` per second. |
| `dbos_conductor_v1_workflow_cancelled_rate` | Rate | `application`, `workflow_name`, `queue_name` | Executor dispatch (`get_workflow_aggregates`, completed window, `CANCELLED`) | Supported | Workflows cancelled per second. |
| `dbos_conductor_v1_workflow_enqueued_count` | Point-in-time | `application`, `workflow_name`, `queue_name` | Executor dispatch (`get_workflow_aggregates`, status `ENQUEUED`) | Supported | Current number of workflows in `ENQUEUED` state. |
| `dbos_conductor_v1_workflow_pending_count` | Point-in-time | `application`, `workflow_name` | Executor dispatch (`get_workflow_aggregates`, status `PENDING`) | Supported | Current number of workflows in `PENDING` (executing) state. |
| `dbos_conductor_v1_workflow_oldest_enqueued_timestamp_seconds` | Point-in-time | `application`, `workflow_name`, `queue_name` | Executor dispatch (`get_workflow_aggregates`, status `ENQUEUED`, `select_min_created_at`) | Supported | Unix timestamp (seconds) of oldest currently enqueued workflow. Series omitted if queue is empty. Age derived via `time() - metric`. |
| `dbos_conductor_v1_workflow_oldest_pending_timestamp_seconds` | Point-in-time | `application`, `workflow_name` | Executor dispatch (`get_workflow_aggregates`, status `PENDING`, `select_min_created_at`) | Supported | Unix timestamp (seconds) of oldest currently executing workflow. Series omitted if no workflows are pending. Age derived via `time() - metric`. |
| `dbos_conductor_v1_workflow_max_queue_wait_seconds` | Windowed | `application`, `workflow_name`, `queue_name` | Executor dispatch (`get_workflow_aggregates`, completed successfully in window) | Supported | Maximum queue wait (created to first started) in seconds across workflows completing in the window. |
| `dbos_conductor_v1_workflow_max_total_latency_seconds` | Windowed | `application`, `workflow_name`, `queue_name` | Executor dispatch (`get_workflow_aggregates`, completed successfully in window) | Supported | Maximum total latency (created to completed) in seconds across workflows completing in the window. |
| `dbos_conductor_v1_step_success_rate` | Rate | `application`, `step_name` | Executor dispatch (`get_step_aggregates`, `SUCCESS`) | Supported | Steps completed successfully per second. |
| `dbos_conductor_v1_step_failed_rate` | Rate | `application`, `step_name` | Executor dispatch (`get_step_aggregates`, statuses other than `SUCCESS`, treated as errors) | Supported | Steps terminating with an error per second. |
| `dbos_conductor_v1_step_max_duration_seconds` | Windowed | `application`, `step_name` | Executor dispatch (`get_step_aggregates`, `SUCCESS`, `select_max_duration_ms`) | Supported | Maximum single-step duration in seconds across steps completing successfully in the window. |

## REST metrics API

In addition to the Prometheus scrape endpoint, Conductor provides a REST API to query historical metrics.

### Operation: `listMetrics`

* **Method**: `GET`
* **Path**: `/v2/orgs/{orgName}/apps/{appName}/metrics`
* **Query Parameters**:
  * `startTime` (required, ISO 8601 / RFC 3339 date-time)
  * `endTime` (required, ISO 8601 / RFC 3339 date-time)
* **Response**: `200 OK` with JSON array of `Metric` objects.

### Schema: `Metric`

```json
{
  "type": "object",
  "required": ["appId", "metricType", "metricName", "granularity", "timeBucket", "value"],
  "properties": {
    "appId": { "type": "string" },
    "metricType": {
      "type": "string",
      "description": "Known values: workflow_count, step_count (reported by executor SDKs), recovery_count (reported by Conductor)."
    },
    "metricName": { "type": "string" },
    "granularity": { "type": "integer", "format": "int32" },
    "timeBucket": { "type": "string", "format": "date-time" },
    "value": { "type": "integer", "format": "int64" }
  },
  "additionalProperties": false
}
```

### Wire protocol correlation for metrics queries

When a client queries `/v2/orgs/{orgName}/apps/{appName}/metrics`, Relay dispatches a `get_metrics` request over the WebSocket hub to an active executor for the application:

```json
{
  "type": "get_metrics",
  "request_id": "<uuid>",
  "start_time": "<RFC3339>",
  "end_time": "<RFC3339>",
  "metric_class": "<class>",
  "application_name": ["<app_name>"]
}
```

The executor queries its local system database (`SysDB.GetMetrics`) and replies:

```json
{
  "type": "get_metrics",
  "request_id": "<uuid>",
  "metrics": [
    {
      "metric_name": "<name>",
      "metric_type": "workflow_count",
      "value": 42.0
    }
  ]
}
```

The control plane merges executor-reported metrics (`workflow_count`, `step_count`) with control plane metrics (`recovery_count` tracked in Relay's store) to construct the REST response.

## Alerting rules and delivery

Conductor provides managed alerting rules that trigger when failure or latency thresholds are breached.

### Alerting REST operations

* **List rules**: `GET /v2/orgs/{orgName}/apps/{appName}/alerting-rules` (`listAlertingRules`) -> `200 OK` array of `AlertingRule`.
* **Create rule**: `POST /v2/orgs/{orgName}/apps/{appName}/alerting-rules` (`createAlertingRule`) -> `201 Created` with `AlertingRule`.
* **Delete rule**: `DELETE /v2/orgs/{orgName}/apps/{appName}/alerting-rules/{ruleId}` (`deleteAlertingRule`) -> `204 No Content`.

### Alerting REST schemas

#### Schema: `CreateAlertInputBody`

```json
{
  "type": "object",
  "required": ["ruleType", "ruleMetadata"],
  "properties": {
    "ruleType": {
      "type": "string",
      "enum": ["WorkflowFailure", "SlowQueue", "UnresponsiveApplication"]
    },
    "ruleMetadata": { "type": "object" },
    "minIntervalSecs": { "type": "integer", "format": "int32" },
    "receivingAppName": { "type": "string" }
  },
  "additionalProperties": false
}
```

#### Schema: `AlertingRule`

```json
{
  "type": "object",
  "required": [
    "id",
    "appId",
    "receivingAppId",
    "ruleType",
    "ruleMetadata",
    "minIntervalSecs",
    "lastFiredAt"
  ],
  "properties": {
    "id": { "type": "string" },
    "appId": { "type": "string" },
    "receivingAppId": { "type": "string" },
    "ruleType": {
      "type": "string",
      "enum": ["WorkflowFailure", "SlowQueue", "UnresponsiveApplication"]
    },
    "ruleMetadata": { "type": "object" },
    "minIntervalSecs": { "type": ["integer", "null"], "format": "int32" },
    "lastFiredAt": { "type": ["string", "null"], "format": "date-time" }
  }
}
```

### Rule types and metadata specifications

The `ruleMetadata` object holds condition thresholds and evaluation parameters. Values in the metadata payload delivered to handlers are string-encoded:

1. **`WorkflowFailure`**:
   * **Condition**: Triggered when the number of failed workflows exceeds a count threshold in a configured time window.
   * **Scope**: Filtered by `workflow_name` (or `*` for all workflows).
   * **Delivered Metadata Keys**:
     * `workflow_name`: Workflow name filter or `*`.
     * `failed_workflow_count`: String count of failures observed.
     * `threshold`: String count threshold.
     * `period_secs`: String time window duration in seconds.

2. **`SlowQueue`**:
   * **Condition**: Triggered when any workflow remains in a queue longer than a duration threshold.
   * **Scope**: Filtered by `queue_name` (or `*` for all queues).
   * **Delivered Metadata Keys**:
     * `queue_name`: Queue name filter or `*`.
     * `stuck_workflow_count`: String count of stuck workflows.
     * `threshold_secs`: String queue wait threshold in seconds.

3. **`UnresponsiveApplication`**:
   * **Condition**: Triggered when an application has no connected executors, or all connected executors fail health checks.
   * **Scope**: Application level.
   * **Routing Requirement**: Because the monitored application is offline, `receivingAppName` must specify a distinct active application to receive the alert.
   * **Delivered Metadata Keys**:
     * `application_name`: Name of the unresponsive application.
     * `connected_executor_count`: String count of connected executors (typically `"0"`).

Per ADR 0008, Relay aligns its alerting rule types strictly with the upstream OpenAPI specification enum (`WorkflowFailure`, `SlowQueue`, `UnresponsiveApplication`). Declarative manifests and REST write endpoints enforce this enum, rejecting unspec'd rule types at configuration time to prevent schema drift and client validation failures.

### Alert delivery wire protocol

When an alert condition is met and the minimum interval (`minIntervalSecs`) has elapsed since `lastFiredAt`, Relay delivers the alert over the WebSocket connection of the designated receiving application.

#### WebSocket notification: `alert`

```json
{
  "type": "alert",
  "request_id": "7b79d282-3e28-4e56-91e8-3a9a7a92b02a",
  "name": "WorkflowFailure",
  "message": "Workflow failure threshold exceeded",
  "metadata": {
    "workflow_name": "processPayment",
    "failed_workflow_count": "5",
    "threshold": "3",
    "period_secs": "60"
  }
}
```

Per the normative WebSocket protocol specification (`docs/protocol/executor-ws.md:209-212`), `alert` is a unidirectional notification frame dispatched by Relay to connected executors with `{name, message, metadata}` (`conductor_protocol.go:590-595`). No response frame is required or expected. If an envelope error occurs during delivery, the wire envelope error field is `error_message` (never `error` or `payload`).

If the application has registered an alert handler (`@DBOS.alert_handler`, `dbos.SetAlertHandler`, `DBOS.setAlertHandler`), the handler executes. If no handler is registered, the SDK automatically logs the alert as a warning.

## Relay control plane extensions: External alert channels

In addition to upstream WebSocket delivery to connected executors, Relay supports dispatching alert notifications to external operational channels configured in operator manifests (`relay.yaml`).

### Supported external channel types

1. **`webhook`**: Generic HTTP POST webhook with replay protection.
   * `url`: Target endpoint URL (HTTP or HTTPS).
   * `secret`: Optional HMAC-SHA256 secret (or resolved via `secret_from`).
   * Headers:
     * `X-Relay-Timestamp`: Unix epoch timestamp in seconds.
     * `X-Relay-Signature`: `sha256=` followed by hex-encoded HMAC-SHA256 of `<timestamp>.<payload>`.
     * `User-Agent`: `relay-alerting/1.0`.
2. **`slack`**: Slack Incoming Webhooks format.
   * `url`: Webhook URL.
   * Delivers JSON with formatted `text` summarizing the alert.
3. **`pagerduty`**: PagerDuty Events API v2 format.
   * `url`: Endpoint URL (defaults to `https://events.pagerduty.com/v2/enqueue`).
   * `routing_key`: Integration routing key.
   * Delivers `event_action: "trigger"` with alert metadata.

### Security and secret resolution

External channel destinations and secret resolution (`secret_from.env`, `secret_from.file`) are restricted exclusively to operator declarative manifests. Tenant-facing REST APIs strictly disallow filesystem probing, and secrets are redacted from all API read responses.

## High availability peer forwarding conventions

In high-availability (HA) multi-node deployments, Relay nodes run behind a load balancer and share a common Postgres store.

### Cluster connection topology

1. **Executor Ownership**:
   * Each executor establishes an outbound WebSocket connection to the cluster.
   * The load balancer routes the connection to any Relay node.
   * The receiving node records ownership and a connection lease in the shared Postgres database (`instances` table). Surviving instances adopt expired leases upon heartbeat timeout.
   * Exactly one Relay node owns an executor WebSocket at any given time.
2. **Inbound Dispatch and Forwarding**:
   * External client requests (from `dbosctl`, the dashboard, or HTTP APIs) land on an arbitrary Relay node via the load balancer.
   * If the target executor is owned by the node that received the HTTP request, it dispatches the message directly across the local WebSocket.
   * If the target executor is owned by a peer node, the local node reads the owner address from the database, signs the payload using HMAC-SHA256 with a timestamp, and forwards the request directly to `/internal/v1/forward/{appID}` on that peer over HTTP.
   * Forwarded requests carry an `X-Relay-Forward-Hop` header. Requests with `hop >= 1` are rejected with HTTP 409 Conflict to prevent forwarding loops. Signatures verify with a maximum allowed timestamp drift of 30 seconds.
   * The owning peer dispatches the request to the executor over the WebSocket, receives the reply, and responds to the forwarding peer.

### Peer forwarding addressing and environment configuration

Peer-to-peer forwarding traffic flows directly between Relay nodes and does not traverse the external load balancer.

| Environment Variable | Default Value | Description |
| --- | --- | --- |
| `DBOS__ADVERTISE_ADDRESS` | `127.0.0.1` | Routable IP address or hostname peers use to forward requests to this instance. Must be set to a cluster-routable address in multi-node deployments. Do not include a port number. |
| `DBOS__CONDUCTOR_PORT` | `8090` | TCP port Conductor/Relay listens on and advertises to peer nodes for internal forwarding. |

### Network requirements

* **Peer Routability**: Every node in the cluster must be able to establish direct TCP connections to `<DBOS__ADVERTISE_ADDRESS>:<DBOS__CONDUCTOR_PORT>` on all peer nodes.
* **Database Consistency**: All nodes must connect to the same PostgreSQL instance or high-availability database cluster (`DBOS__CONDUCTOR_DB_URL`).
* **Authentication and Integrity**: Internal peer requests are authenticated via HMAC-SHA256 signatures, validated with bounded timestamp drift (30 seconds) and loop prevention headers (`X-Relay-Forward-Hop`).
