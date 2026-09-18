---
title: Wire to REST Mapping
type: Reference
---
# Wire to REST Mapping

This reference maps Conductor executor WebSocket messages to OpenAPI REST schemas from `api/spec/openapi-3.0.json`.

Relay decouples HTTP clients from live executors through an internal router. The router converts REST parameters into protocol envelopes, routes messages across WebSocket connections, and maps responses into OpenAPI models.

## Workflows

### Search and List

Incoming REST search parameters map to `ListWorkflowsRequest` body:
* `workflowIds` -> `WorkflowUUIDs` (`workflow_uuids`, list)
* `workflowName` -> `WorkflowName`
* `status` -> `Status` (list or string)
* `user` -> `AuthenticatedUser`
* `startTime` / `endTime` -> `StartTime` / `EndTime`
* `limit` -> `Limit`

Wire `ListWorkflowsResponseBody` maps to OpenAPI `#/components/schemas/Workflow`:
* `WorkflowUUID` -> `workflowId`
* `Status` -> `status`
* `WorkflowName` -> `workflowName` (null if absent)
* `WorkflowClassName` -> `workflow_class` (null if absent)
* `WorkflowConfigName` -> `workflow_config` (null if absent)
* `AuthenticatedUser` -> `user` (null if absent)
* `AssumedRole` -> `assumedRole` (null if absent)
* `AuthenticatedRoles` -> `roles` (null if absent)
* `Input` -> `input` (null if absent)
* `Output` -> `output` (null if absent)
* `Error` -> `error` (null if absent)
* `CreatedAt` -> `createdAt` (RFC 3339 timestamp)
* `UpdatedAt` -> `updatedAt` (RFC 3339 timestamp)
* `QueueName` -> `queueName` (null if absent)
* `ApplicationVersion` -> `appVersion` (null if absent)
* `ExecutorID` -> `executorId` (null if absent)
* `WorkflowTimeoutMS` -> `timeoutMs` (parsed integer, null if absent)
* `WasForkedFrom` / `ForkedFrom` -> `wasForkedFrom` (boolean) / `forkedFrom` (null if absent)
* `ParentWorkflowID` -> `parentWorkflowId` (null if absent)

## Workflow Steps

REST query `GET /v2/orgs/{org}/apps/{app}/workflows/{workflowId}/steps` maps to `ListStepsRequest`:
* Path `workflowId` -> `WorkflowID`
* Query `limit` -> `Limit`
* Query `offset` -> `Offset`

Wire `WorkflowStepsResponseBody` maps to OpenAPI `#/components/schemas/WorkflowStep`:
* `FunctionID` -> `stepId` (integer)
* `FunctionName` -> `stepName` (string)
* `Output` -> `output` (null if absent)
* `Error` -> `error` (null if absent)
* `ChildWorkflowID` -> `childWorkflowId` (null if absent)
* `StartedAtEpochMs` -> `startedAt` (RFC 3339 timestamp)
* `CompletedAtEpochMs` -> `completedAt` (RFC 3339 timestamp)

## Events and Notifications

* `GetWorkflowEventsRequest` retrieves custom events. Wire `EventOutput` maps `Key` and `Value` to OpenAPI `EventInformation`.
* `GetWorkflowNotificationsRequest` retrieves pending or consumed workflow notifications. Wire `NotificationOutput` maps `Topic`, `Message`, `CreatedAtEpochMs`, and `Consumed` to OpenAPI `NotificationInformation`.

## Real-Time Telemetry (Server-Sent Events)

Relay provides HTTP Server-Sent Events (SSE) endpoints for reactive UI telemetry:
* `GET /v2/orgs/{orgName}/apps/{appName}/events`: Streams real-time lifecycle updates for an application.
* `GET /v2/orgs/{orgName}/events`: Streams organization-wide lifecycle updates.

Clients authenticate with standard `Authorization: Bearer <token>` headers or via `?token=<token>` query parameters for browser `EventSource` compatibility.

The stream emits chunked `text/event-stream` payloads:
* `ready`: Emitted on stream connection confirmation.
* `workflow_update`: Emitted when workflows start, finish, or transition status.
* `ping`: Heartbeat event emitted every 15 seconds to maintain open transport connections.

## Queues

Wire `QueueOutput` maps to OpenAPI `#/components/schemas/Queue`:
* `Name` -> `name`
* `Concurrency` -> `concurrency`
* `WorkerConcurrency` -> `workerConcurrency`
* `RateLimitMax` -> `rateLimitMax`
* `RateLimitPeriodSec` -> `rateLimitPeriodSecs`
* `PriorityEnabled` -> `priorityEnabled`
* `PartitionQueue` -> `partitionQueue`
* `PollingIntervalSec` -> `pollingIntervalSecs`
* `ApplicationName` -> `applicationName`

## Schedules

Wire `ScheduleOutput` maps to OpenAPI `#/components/schemas/Schedule`:
* `ScheduleID` -> `scheduleId`
* `ScheduleName` -> `scheduleName`
* `WorkflowName` -> `workflowName`
* `WorkflowClassName` -> `workflowClass`
* `Schedule` -> `cronExpression`
* `Status` -> `status`
* `Context` -> `context`
* `LastFiredAt` -> `lastFiredAt` (RFC 3339 timestamp)
* `AutomaticBackfill` -> `automaticBackfill`
* `CronTimezone` -> `cronTimezone`
* `ApplicationName` -> `applicationName`

Wire `queue_name` is dropped because OpenAPI `#/components/schemas/Schedule` has no corresponding property.

## Metrics

REST query `GET /v2/orgs/{org}/apps/{app}/metrics` maps to `GetMetricsRequest`:
* Query `startTime` -> `StartTime` (`start_time`, RFC 3339 timestamp)
* Query `endTime` -> `EndTime` (`end_time`, RFC 3339 timestamp)
* Query metric class -> `MetricClass` (`metric_class`, string)
* Path `app` -> `ApplicationName` (`application_name`, list)

Wire `GetMetricsResponse` contains `metrics` (array of `MetricData` with `metric_name`, `metric_type`, `value`), mapped to OpenAPI `#/components/schemas/Metric`:
* `MetricName` -> `metricName`
* `MetricType` -> `metricType`
* `Value` -> `value`

## Error Mapping

All REST API errors map to RFC 9457 Problem Details (`application/problem+json`):
* `router.ErrAppNotFound` -> HTTP 404 Problem Details
* `router.ErrNoLiveExecutor` -> HTTP 503 Problem Details (service unavailable)
* `router.ErrExecutorTimeout` -> HTTP 504 Problem Details (gateway timeout)
* `router.ErrExecutorError` -> HTTP 400 or HTTP 400 Problem Details
* Malformed JSON or validation errors -> HTTP 400 Problem Details
