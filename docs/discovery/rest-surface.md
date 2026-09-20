---
type: Reference
title: REST API surface and operation inventory
description: Complete inventory of Conductor v2 REST operations, data storage tiering, schema constraints, and wire mappings.
status: draft
---

# REST API surface and operation inventory

Relay implements the DBOS Conductor v2 HTTP REST contract to provide drop-in
compatibility for unmodified DBOS Transact applications and client tooling.
This document inventories every operation defined by the vendored OpenAPI
contract, records where the backing data lives (Relay Postgres store versus
live dispatch to an executor over WebSockets), identifies OIDC-gated endpoints,
documents naming and error constraints, and catalogues schema fields that Relay
does not populate.

## Specification provenance

The REST surface is defined by two OpenAPI documents vendored under `api/spec/`:
* `openapi.json` (OpenAPI 3.1.0, SHA256: `b5dc31eb29686a84fe0390a7446b5acdbc0dd05846cc94a746de649b92880722`)
* `openapi-3.0.json` (OpenAPI 3.0.3, SHA256: `aed633d5b923e24b1c27e0860ca38af00c0941747fbc7bfed1d9fb4de3fdfd4a`)

Both documents were fetched on 2026-09-08 from `https://cloud.dbos.dev/conductor/v2/`.
Both were served publicly over HTTPS with HTTP 200 OK without requiring authentication
or click-through licensing. Detailed provenance
and checksums are tracked in `api/spec/PROVENANCE.md` and the
[provenance ledger](provenance.md).

## Operation inventory

The OpenAPI specification defines 64 operations across applications, workflows,
steps, queues, schedules, executors, alerting rules, metrics, organizations,
members, roles, and tokens.

Per Relay architectural invariants and ADR 0004, Relay never executes direct raw
SQL against an application's system database. Workflow, step, queue, and schedule
data are fetched on demand from executors over the WebSocket connection hub, or
via the official SDK client when an application has an explicitly configured data-plane
connection (with live executors taking precedence). System metadata (applications,
versions, API keys, alert rules, and executor registrations) is stored in Relay's
own Postgres database.

| Method | Path | Operation ID | Storage Location | OIDC Required | Wire Message |
| --- | --- | --- | --- | --- | --- |
| `GET` | `/v2/orgs/{orgName}` | `getOrg` | Relay store | Yes | N/A |
| `PATCH` | `/v2/orgs/{orgName}` | `updateOrg` | Relay store | Yes | N/A |
| `GET` | `/v2/orgs/{orgName}/apps` | `listApps` | Relay store | No | N/A |
| `DELETE` | `/v2/orgs/{orgName}/apps/{appName}` | `deleteApp` | Relay store | No | N/A |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}` | `getApp` | Relay store | No | N/A |
| `PATCH` | `/v2/orgs/{orgName}/apps/{appName}` | `updateApp` | Relay store | No | N/A |
| `PUT` | `/v2/orgs/{orgName}/apps/{appName}` | `registerApp` | Relay store | No | N/A |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/alerting-rules` | `listAlertingRules` | Relay store | No | N/A |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/alerting-rules` | `createAlertingRule` | Relay store | No | N/A |
| `DELETE` | `/v2/orgs/{orgName}/apps/{appName}/alerting-rules/{ruleId}` | `deleteAlertingRule` | Relay store | No | N/A |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/autoscale` | `getAutoscale` | Not implemented (Tier 5) | No | N/A |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/autoscale/versions/{version}` | `getAutoscaleVersion` | Not implemented (Tier 5) | No | N/A |
| `DELETE` | `/v2/orgs/{orgName}/apps/{appName}/autoscaling-policy` | `deleteAutoscalingPolicy` | Not implemented (Tier 5) | No | N/A |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/autoscaling-policy` | `getAutoscalingPolicy` | Not implemented (Tier 5) | No | N/A |
| `PUT` | `/v2/orgs/{orgName}/apps/{appName}/autoscaling-policy` | `setAutoscalingPolicy` | Not implemented (Tier 5) | No | N/A |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/executors` | `listExecutors` | Relay store | No | N/A |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/metrics` | `listMetrics` | Relay store | No | N/A |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/queues` | `listQueues` | Executor dispatch | No | `ListQueuesRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/queues/{queueName}` | `getQueue` | Executor dispatch | No | `GetQueueRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/schedules` | `listSchedules` | Executor dispatch | No | `ListSchedulesRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}` | `getSchedule` | Executor dispatch | No | `GetScheduleRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/backfill` | `backfillSchedule` | Executor dispatch | No | `BackfillScheduleRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/pause` | `pauseSchedule` | Executor dispatch | No | `PauseScheduleRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/resume` | `resumeSchedule` | Executor dispatch | No | `ResumeScheduleRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/trigger` | `triggerSchedule` | Executor dispatch | No | `TriggerScheduleRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/steps/aggregates` | `getStepAggregates` | Executor dispatch | No | `GetStepAggregatesRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/versions` | `listAppVersions` | Relay store | No | N/A |
| `PATCH` | `/v2/orgs/{orgName}/apps/{appName}/versions/latest` | `setLatestAppVersion` | Relay store | No | N/A |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows` | `listWorkflows` | Executor dispatch | No | `ListWorkflowsRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/aggregates` | `getWorkflowAggregates` | Executor dispatch | No | `GetWorkflowAggregatesRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/bulk-cancel` | `bulkCancelWorkflows` | Executor dispatch | No | `CancelWorkflowRequest` (bulk) |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/bulk-delete` | `bulkDeleteWorkflows` | Executor dispatch | No | `DeleteWorkflowRequest` (bulk) |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/bulk-fork-from-failure` | `bulkForkWorkflowsFromFailure` | Executor dispatch | No | `ForkFromFailureRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/bulk-resume` | `bulkResumeWorkflows` | Executor dispatch | No | `ResumeWorkflowRequest` (bulk) |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/import` | `importWorkflow` | Executor dispatch | No | `ImportWorkflowRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/search` | `searchWorkflows` | Executor dispatch | No | `ListWorkflowsRequest` |
| `DELETE` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}` | `deleteWorkflow` | Executor dispatch | No | `DeleteWorkflowRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}` | `getWorkflow` | Executor dispatch | No | `GetWorkflowRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/cancel` | `cancelWorkflow` | Executor dispatch | No | `CancelWorkflowRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/events` | `listWorkflowEvents` | Executor dispatch | No | `GetWorkflowEventsRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/export` | `exportWorkflow` | Executor dispatch | No | `ExportWorkflowRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/fork` | `forkWorkflow` | Executor dispatch | No | `ForkWorkflowRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/notifications` | `listWorkflowNotifications` | Executor dispatch | No | `GetWorkflowNotificationsRequest` |
| `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/resume` | `resumeWorkflow` | Executor dispatch | No | `ResumeWorkflowRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/steps` | `listWorkflowSteps` | Executor dispatch | No | `ListStepsRequest` |
| `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/streams` | `listWorkflowStreams` | Executor dispatch | No | `GetWorkflowStreamsRequest` |
| `GET` | `/v2/orgs/{orgName}/audit-logs` | `listAuditLogs` | Relay store | Yes | N/A |
| `GET` | `/v2/orgs/{orgName}/domain-claims` | `listDomainClaims` | Relay store | Yes | N/A |
| `POST` | `/v2/orgs/{orgName}/domain-claims` | `requestDomainClaim` | Relay store | Yes | N/A |
| `DELETE` | `/v2/orgs/{orgName}/domain-claims/{domain}` | `releaseDomainClaim` | Relay store | Yes | N/A |
| `POST` | `/v2/orgs/{orgName}/join` | `joinOrg` | Relay store | Yes | N/A |
| `GET` | `/v2/orgs/{orgName}/members` | `listMembers` | Relay store | Yes | N/A |
| `DELETE` | `/v2/orgs/{orgName}/members/{username}` | `removeMember` | Relay store | Yes | N/A |
| `PUT` | `/v2/orgs/{orgName}/members/{username}/roles/{roleName}` | `grantRole` | Relay store | Yes | N/A |
| `GET` | `/v2/orgs/{orgName}/permissions` | `listPermissions` | Relay store | No | N/A |
| `GET` | `/v2/orgs/{orgName}/roles` | `listRoles` | Relay store | Yes | N/A |
| `POST` | `/v2/orgs/{orgName}/roles` | `createRole` | Relay store | Yes | N/A |
| `DELETE` | `/v2/orgs/{orgName}/roles/{roleName}` | `deleteRole` | Relay store | Yes | N/A |
| `POST` | `/v2/orgs/{orgName}/secrets` | `generateSecret` | Relay store | Yes | N/A |
| `GET` | `/v2/orgs/{orgName}/tokens` | `listTokens` | Relay store | No | N/A |
| `DELETE` | `/v2/orgs/{orgName}/tokens/{tokenName}` | `deleteToken` | Relay store | No | N/A |
| `POST` | `/v2/orgs/{orgName}/tokens/{tokenName}` | `createToken` | Relay store | No | N/A |
| `POST` | `/v2/users` | `registerUser` | Relay store | Yes | N/A |
| `GET` | `/v2/users/me` | `getCurrentUser` | Relay store | Yes | N/A |

## Schema and parameter constraints

The OpenAPI document defines strict validation patterns and formats across
common identifiers and error models.

### Identifier constraints

1. **Organization name (`orgName`)**:
   * Minimum length: 3
   * Maximum length: 30
   * Character pattern: `^[a-z0-9_]+$`
   * Combined regular expression: `^[a-z0-9_]{3,30}$`
   * Applies to all `/v2/orgs/{orgName}/...` path segments.

2. **Application name (`appName`)**:
   * Minimum length: 3
   * Maximum length: 30 in the REST path parameter schema (extending up to 256
     characters in backend workflow contexts and version identifiers).
   * Character pattern: `^[a-z0-9-_]+$`
   * Combined regular expression: `^[a-z0-9-_]{3,30}$` (path parameter) and
     `^[a-z0-9-_]{3,256}$` (general storage validation).
   * Allows lowercase alphanumeric characters, hyphens, and underscores.

### Error response format

Every non-success response across the entire OpenAPI specification is typed as
`application/problem+json` referencing the `ErrorModel` schema, complying with
RFC 9457 (Problem Details for HTTP APIs):

```json
{
  "$schema": "//schemas/ErrorModel.json",
  "type": "about:blank",
  "title": "Bad Request",
  "status": 400,
  "detail": "Human-readable explanation of the specific error.",
  "instance": "https://example.com/error-log/abc123",
  "errors": [
    {
      "location": "body.appName",
      "message": "appName does not match pattern",
      "value": "INVALID NAME"
    }
  ]
}
```

Key fields in `ErrorModel`:
* `type`: URI reference to error documentation (default: `about:blank`).
* `title`: Short summary of the problem type (does not change across instances).
* `status`: HTTP status code integer.
* `detail`: Explanation specific to this occurrence.
* `instance`: URI reference identifying this occurrence.
* `errors`: Optional array of `ErrorDetail` objects containing `location`
  (path or body selector), `message` (error explanation), and `value` (offending input).

## OIDC and authentication segmentation

The specification includes 16 operations tagged with `x-dbos-requires-oauth: true`:
* User registration and current profile: `POST /v2/users`, `GET /v2/users/me`
* Organization management: `GET /v2/orgs/{orgName}`, `PATCH /v2/orgs/{orgName}`,
  `POST /v2/orgs/{orgName}/join`, `POST /v2/orgs/{orgName}/secrets`
* Member management: `GET /v2/orgs/{orgName}/members`,
  `DELETE /v2/orgs/{orgName}/members/{username}`,
  `PUT /v2/orgs/{orgName}/members/{username}/roles/{roleName}`
* Role management: `GET /v2/orgs/{orgName}/roles`, `POST /v2/orgs/{orgName}/roles`,
  `DELETE /v2/orgs/{orgName}/roles/{roleName}`
* Domain claims: `GET /v2/orgs/{orgName}/domain-claims`,
  `POST /v2/orgs/{orgName}/domain-claims`,
  `DELETE /v2/orgs/{orgName}/domain-claims/{domain}`
* Audit logging: `GET /v2/orgs/{orgName}/audit-logs`

In self-hosted no-auth mode (Tier 1 through Tier 6), these routes respond with
HTTP 404 Problem Details rather than 403 Forbidden, conforming to upstream
client expectations when running without an identity provider.

## Schemas Relay does not populate

Several schemas in the vendored specification represent DBOS Cloud proprietary
managed infrastructure (such as managed RDS, managed VMs, and cloud domain routing)
or are deferred to later compatibility tiers:

1. **Cloud infrastructure fields in `Organization`**:
   * `defaultRdsInstanceClassType`: Proprietary RDS instance sizing.
   * `maxApps`, `maxConductorApps`, `maxDatabaseInstances`, `maxVms`, `maxTeamMembers`:
     Cloud tenant quota limits.
   * `subscriptionPlan`: DBOS Cloud billing tier (e.g. "free", "pro", "teams").
   * Relay returns sensible defaults (or null) for self-hosted instances rather
     than emulating proprietary billing entities.

2. **Autoscaling policies and recommendations (`AutoscalePolicy`, `QueueAutoscale`, `RolloutPolicy`)**:
   * `QueueAutoscale` computes recommended replica counts for external scalers (KEDA).
   * Deferred to Tier 5 (Scale), as standalone self-hosted deployments rely on
     external container orchestrators or fixed executor processes.
   * Relay returns 404 Problem Details when reading autoscale recommendations or policies for an application (and 400 on attempts to write an autoscaling policy), matching upstream behaviour when no policy is configured.

3. **Domain claims (`DomainClaim`)**:
   * `requestDomainClaim`, `listDomainClaims`, `releaseDomainClaim`.
   * On DBOS-managed Conductor these manage DNS and TLS termination on
     cloud infrastructure with vendor approval. On self-hosted Relay they
     take effect immediately and drive local organization auto-enrollment.

4. **Cloud-specific fields in `UserProfile`**:
   * `isDbosAdmin`, `subscriptionPlan`.

5. **Cloud host identifier in `Executor`**:
   * `hostId` represents cloud VM instance identifiers; Relay populates `executorId`,
     `hostname`, `appVersion`, `language`, and `executorMetadata` from executor
     registrations.
