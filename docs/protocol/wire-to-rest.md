---
title: Wire to REST Mapping
type: Reference
---
# Wire to REST Mapping

This maps wire WebSocket types to OpenAPI REST schemas from `api/spec/openapi.json`.

## Workflow Status

Wire type fields map to `WorkflowInformation`:
* `workflow_uuid` -> `workflowUUID`
* `status` -> `status`
* `name` -> `workflowName`
* `class_name` -> `workflowClassName`
* `config_name` -> `workflowConfigName`
* `output` -> `output`
* `error` -> `error`

## Steps

Wire type `StepStatus` maps to `StepInformation`.

## Events and Notifications

Mapped to `EventInformation` and `NotificationInformation` appropriately based on the fields defined in the SDK schemas.
