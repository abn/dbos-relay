---
type: Reference
---

# Conformance Batteries Specification

Relay validates Conductor wire protocol and HTTP API compatibility through eight
conformance test batteries. These batteries run against either an in-process Relay
server or any live endpoint to verify behavioral parity with DBOS Conductor.

## Synthetic Peer Disclosure

Batteries 2 through 6 test control plane framing, dispatch, and lifecycle handling
by connecting an in-tree synthetic test double (`internal/fakeexecutor`). The synthetic
peer implements the DBOS executor WebSocket wire protocol (D1, D2) and registers
handlers that assert control frame reception and reply with typed responses.

These batteries certify control plane correctness, REST-to-WebSocket multiplexing,
and recovery state transitions. Verification against external, unmodified DBOS Transact
SDK runtimes is handled separately by the SDK verification test suite (`tests/verifysdk`).

## Battery Catalog

### Battery 1: Specification and System Probes

Validates discovery, OpenAPI specifications, documentation, and system metrics endpoints.

- **Authentication Requirements**:
  - Checks 1.1 through 1.5 evaluate public discovery and OpenAPI documentation endpoints (`/healthz`, `/openapi.json`, `/openapi-3.0.json`, `/openapi.yaml`, `/docs`); no credentials or tokens are required.
  - Check 1.6 (`GET /v1/metrics`) supports both authenticated and unauthenticated execution modes:
    - In authenticated runs (where a bearer token or Conductor key is configured), Check 1.6 tests that requests without credentials receive HTTP 401 Unauthorized (when authentication is enabled on the server) and requests with the valid bearer token (`Authorization: Bearer <key>`) return HTTP 200 OK serving OpenMetrics Prometheus gauges. If the server permits unauthenticated metrics scraping, HTTP 200 OK is accepted directly.
    - In unauthenticated runs (where no key is configured, such as unauthenticated discovery probes or external suites like `tests/verifysdk`), Check 1.6 accepts HTTP 401 Unauthorized as verification that authentication is properly enforced on the metrics endpoint, or HTTP 200 OK if open metrics scraping is permitted.
    - Callers may run Battery 1 with or without credentials.
- **Check 1.1**: `GET /healthz` returns `200 OK` with JSON `{"status":"ok"}` when the database is healthy.
- **Check 1.2**: `GET /openapi.json` returns `200 OK` with a valid OpenAPI 3.1 JSON document.
- **Check 1.3**: `GET /openapi-3.0.json` returns `200 OK` with a valid OpenAPI 3.0 JSON document.
- **Check 1.4**: `GET /openapi.yaml` returns `200 OK` with valid OpenAPI YAML content.
- **Check 1.5**: `GET /docs` returns `200 OK` serving the Swagger UI HTML documentation.
- **Check 1.6**: `GET /v1/metrics` verifies OpenMetrics Prometheus gauge scraping or authenticated endpoint protection.

### Battery 2: WebSocket Handshake and Fleet Registration

Validates executor authentication, protocol upgrade, and fleet catalog registration.

- **Peer**: `internal/fakeexecutor`
- **Authentication Requirements**:
  - Check 2.1 tests WebSocket handshake rejection for unauthorized connections. It tests both unauthenticated connection attempts (missing key in path, `/websocket/{appName}/`) and invalid key attempts (`/websocket/{appName}/{invalidKey}`). Check 2.1 succeeds in both authenticated and unauthenticated test runs because it specifically asserts rejection of missing and invalid credentials.
  - Check 2.2 requires a valid Conductor API key (`ConductorKey` or `RELAY_API_KEY`) scoped to the target application with `websocket.connect` permission. Callers or test suites (such as `tests/verifysdk`) that do not possess a provisioned API key or run without synthetic executors must skip Battery 2 or provide a valid scoped key.
- **Check 2.1 (WebSocket Handshake Rejection)**: Connects to `/websocket/{appName}/` (unauthenticated)
  and `/websocket/{appName}/{invalidKey}` (invalid key). Asserts that each upgrade is rejected before
  completion with HTTP 401 Unauthorized and an RFC 9457 Problem Details body (`application/problem+json`)
  containing `title` and `status`.
- **Check 2.2 (Valid Key Handshake and Fleet Registration)**: Connects to `/websocket/{appName}/{conductorKey}`.
  Completes WebSocket upgrade, sends `executor_info`, and polls `GET /v2/orgs/{org}/apps/{app}/executors`
  until the executor appears with status `HEALTHY`.

### Battery 3: REST and Wire Multiplexing (Observability)

Validates that read-only REST observability queries dispatch typed WebSocket frames to active
executors and serialize responses according to OpenAPI schemas.

- **Peer**: `internal/fakeexecutor`
- **Check 3.1 (List Workflows)**: `GET /v2/orgs/{org}/apps/{app}/workflows` dispatches
  `list_workflows` to the connected executor and verifies response workflows.
- **Check 3.2 (Get Workflow)**: `GET /v2/orgs/{org}/apps/{app}/workflows/{workflowId}`
  dispatches `get_workflow` and validates the returned workflow object.
- **Check 3.3 (List Workflow Steps)**: `GET /v2/orgs/{org}/apps/{app}/workflows/{workflowId}/steps`
  dispatches `list_steps` and verifies step execution metadata.

### Battery 4: Workflow Control Operations

Validates mutating workflow control actions dispatched from the control plane to executors.

- **Peer**: `internal/fakeexecutor`
- **Check 4.1 (Cancel Workflow)**: `POST /v2/orgs/{org}/apps/{app}/workflows/{workflowId}/cancel`
  dispatches `cancel` frame to the executor, returning HTTP 200 or 204. Asserts that the executor
  affirmatively received the cancel frame for the targeted workflow.
- **Check 4.2 (Resume Workflow)**: `POST /v2/orgs/{org}/apps/{app}/workflows/{workflowId}/resume`
  dispatches `resume` frame to the executor, returning HTTP 200 or 204. Asserts that the executor
  affirmatively received the resume frame for the targeted workflow.
- **Check 4.3 (Restart/Fork Workflow)**: `POST /v2/orgs/{org}/apps/{app}/workflows/{workflowId}/fork`
  with `newWorkflowId` dispatches `fork_workflow` frame to the executor, returning HTTP 200 or 201
  with the new workflow identifier. Asserts that the executor received the fork request and that
  the response contains the expected workflow ID.

### Battery 5: Queues and Schedules Operations

Validates queue inspection and schedule management commands dispatched to executors.

- **Peer**: `internal/fakeexecutor`
- **Check 5.1 (List Queues)**: `GET /v2/orgs/{org}/apps/{app}/queues` dispatches `list_queues`
  and verifies that registered queues are returned.
- **Check 5.2 (List Schedules)**: `GET /v2/orgs/{org}/apps/{app}/schedules` dispatches `list_schedules`
  and verifies schedule metadata (ID, schedule name, workflow name, cron expression, status).
- **Check 5.3 (Pause Schedule)**: `POST /v2/orgs/{org}/apps/{app}/schedules/{scheduleName}/pause`
  dispatches `pause_schedule` frame to the executor, returning HTTP 200 or 204, and asserts that
  the executor received the pause directive.
- **Check 5.4 (Resume Schedule)**: `POST /v2/orgs/{org}/apps/{app}/schedules/{scheduleName}/resume`
  dispatches `resume_schedule` frame to the executor, returning HTTP 200 or 204, and asserts that
  the executor received the resume directive.

### Battery 6: Workflow Recovery and Liveness Lifecycle

Validates detection of ungraceful executor disconnects and dispatch of workflow recovery tasks.

- **Peer**: `internal/fakeexecutor`
- **Check 6.1 (Disconnect Detection)**: Shuts down an active executor socket, waits for lease expiry,
  and verifies via `GET /v2/orgs/{org}/apps/{app}/executors` that the executor transitions from
  `HEALTHY` to `DISCONNECTED` or `DEAD`.
- **Check 6.2 (Recovery Adoption)**: Connects a second executor and verifies dispatch of `recovery`
  frames instructing the replacement executor to adopt pending workflows from dead nodes.

### Battery 7: Alerting Rules Management

Validates CRUD operations for application-level alert configurations.

- **Check 7.1 (Create Alerting Rule)**: `POST /v2/orgs/{org}/apps/{app}/alerting-rules` creates a
  new alert rule, returning HTTP 200 or 201 with an assigned rule ID and rule type `WorkflowFailure`.
- **Check 7.2 (List Alerting Rules)**: `GET /v2/orgs/{org}/apps/{app}/alerting-rules` returns HTTP 200
  and verifies the created rule is present in the list with matching type.
- **Check 7.3 (Delete Alerting Rule)**: `DELETE /v2/orgs/{org}/apps/{app}/alerting-rules/{ruleId}`
  removes the rule with HTTP 200 or 204, followed by a verification query confirming the rule is absent.

### Battery 8: RFC 9457 Problem Details and Identity Gating

Validates standard error serialization and self-hosted mode endpoint gating.

- **Check 8.1 (Malformed JSON)**: Sending malformed JSON to a write endpoint returns HTTP 400 Bad Request
  with `Content-Type: application/problem+json` and valid RFC 9457 fields (`status: 400`, `title`).
- **Check 8.2 (Non-Existent Resource)**: Querying a non-existent entity returns HTTP 404 Not Found
  with `Content-Type: application/problem+json` and `status: 404`.
- **Check 8.3 (Self-Hosted OAuth Gating)**: Querying unauthenticated user/org endpoints (such as
  `/v2/users/me`) in self-hosted no-auth mode returns HTTP 404 Problem Details rather than HTTP 403 Forbidden.
