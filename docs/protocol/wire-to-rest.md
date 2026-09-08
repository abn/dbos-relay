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
* `workflowIds` -> `WorkflowUUID` (list or string)
* `workflowName` -> `WorkflowName`
* `workflowClass` -> `WorkflowClassName`
* `workflowConfig` -> `WorkflowConfigName`
* `status` -> `Status` (list or string)
* `user` -> `AuthenticatedUser`
* `startTime` / `endTime` -> `StartTime` / `EndTime`
* `limit` -> `Limit`

Wire `ListWorkflowsResponseBody` maps to OpenAPI `#/components/schemas/Workflow`:
* `WorkflowUUID` -> `workflowId`
* `Status` -> `status`
* `WorkflowName` -> `workflowName` (null if absent)
* `WorkflowClassName` -> `workflowClass` (null if absent)
* `WorkflowConfigName` -> `workflowConfig` (null if absent)
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
* `FunctionName` -> `name` (string)
* `Output` -> `output` (null if absent)
* `Error` -> `error` (null if absent)
* `ChildWorkflowID` -> `childWorkflowId` (null if absent)
* `StartedAtEpochMs` -> `startedAt` (RFC 3339 timestamp)
* `CompletedAtEpochMs` -> `completedAt` (RFC 3339 timestamp)

## Events and Notifications

* `GetWorkflowEventsRequest` retrieves custom events. Wire `EventOutput` maps `Key` and `Value` to OpenAPI `EventInformation`.
* `GetWorkflowNotificationsRequest` retrieves pending or consumed workflow notifications. Wire `NotificationOutput` maps `Topic`, `Message`, `CreatedAtEpochMs`, and `Consumed` to OpenAPI `NotificationInformation`.

## Queues

Wire `QueueOutput` maps to OpenAPI `#/components/schemas/Queue`:
* `Name` -> `name`
* `Concurrency` -> `concurrency`
* `WorkerConcurrency` -> `workerConcurrency`
* `RateLimitMax` -> `rateLimitMax`
* `RateLimitPeriodSec` -> `rateLimitPeriodSec`
* `PriorityEnabled` -> `priorityEnabled`

## Schedules

Wire `ScheduleOutput` maps to OpenAPI `#/components/schemas/Schedule`:
* `ScheduleID` -> `id`
* `ScheduleName` -> `name`
* `WorkflowName` -> `workflowName`
* `WorkflowClassName` -> `workflowClassName`
* `Schedule` -> `schedule`
* `Status` -> `status`
* `Context` -> `context`
* `LastFiredAt` -> `lastFiredAt` (RFC 3339 timestamp)
* `AutomaticBackfill` -> `automaticBackfill`
* `CronTimezone` -> `cronTimezone`
* `QueueName` -> `queueName`

## Error Mapping

All REST API errors map to RFC 9457 Problem Details (`application/problem+json`):
* `router.ErrAppNotFound` -> HTTP 404 Problem Details
* `router.ErrNoLiveExecutor` -> HTTP 503 Problem Details (service unavailable)
* `router.ErrExecutorTimeout` -> HTTP 504 Problem Details (gateway timeout)
* `router.ErrExecutorError` -> HTTP 400 or HTTP 502 Problem Details
* Malformed JSON or validation errors -> HTTP 400 Problem Details
