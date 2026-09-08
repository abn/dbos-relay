# Conformance Batteries Reference

Detailed reference for the 8 conformance batteries implemented by Relay and
evaluated by `make test/conformance` and `relay test-conformance`.

---

## Battery 1: Specification & System Probes

Certifies that the control plane serves health indicators, OpenAPI contracts, and
metrics without authentication.

| Check | Path | Method | Expected Status | Contract Assertions |
| :--- | :--- | :---: | :---: | :--- |
| **1.1** | `/healthz` | `GET` | `200 OK` | `{"status":"ok"}`. Service reports healthy database connectivity. |
| **1.2** | `/openapi.json` | `GET` | `200 OK` | Valid JSON document containing OpenAPI 3.1 specification. |
| **1.3** | `/openapi-3.0.json` | `GET` | `200 OK` | Valid JSON document containing OpenAPI 3.0 specification. |
| **1.4** | `/openapi.yaml` | `GET` | `200 OK` | Valid YAML text containing `openapi:` version identifier. |
| **1.5** | `/docs` | `GET` | `200 OK` | HTML documentation page embedding interactive API explorer. |
| **1.6** | `/v1/metrics` | `GET` | `200 OK` | OpenMetrics/Prometheus formatted text exposing control plane gauges. |

---

## Battery 2: WebSocket Handshake & Fleet Registration

Certifies that executor WebSocket connections adhere to authentication, upgrade,
and registration protocols.

- **Upgrade Path**: `/websocket/{appName}/{conductorKey}`
- **Heartbeat Interval**: 20 seconds (`_PING_INTERVAL`)
- **First Frame**: Executor sends `executor_info` response with `executor_id`,
  `application_version`, `language`, and optional `hostname` and `metadata`.

| Check | Scenario | Expected Outcome |
| :--- | :--- | :--- |
| **2.1** | Invalid API Key | Handshake is rejected before upgrade with HTTP 401 Problem Details (`application/problem+json`). |
| **2.2** | Valid Handshake | WebSocket upgrade succeeds; executor appears in `GET /v2/orgs/{org}/apps/{app}/executors` with status `HEALTHY`. |

---

## Battery 3: REST & Wire Multiplexing (Observability)

Certifies that HTTP REST queries are converted to typed WebSocket frames,
dispatched to active executors, and returned as OpenAPI-compliant responses.

| Check | REST Endpoint | Wire Request Type | Wire Response Type | Key Assertions |
| :--- | :--- | :--- | :--- | :--- |
| **3.1** | `GET /v2/orgs/{org}/apps/{app}/workflows` | `list_workflows` | `ListWorkflowsResponse` | Array of workflows with `workflowId`, `workflowName`, `status`. |
| **3.2** | `GET /v2/orgs/{org}/apps/{app}/workflows/{id}` | `get_workflow` | `GetWorkflowResponse` | Single workflow record matching requested ID. |
| **3.3** | `GET /v2/orgs/{org}/apps/{app}/workflows/{id}/steps` | `list_steps` | `ListStepsResponse` | Step execution sequence with function names and statuses. |

---

## Battery 4: Workflow Control Operations

Certifies that mutating workflow operations are dispatched over executor sockets.

| Check | REST Endpoint | Method | Wire Request Type | Expected Status |
| :--- | :--- | :---: | :--- | :---: |
| **4.1** | `/v2/orgs/{org}/apps/{app}/workflows/{id}/cancel` | `POST` | `cancel` (`CancelWorkflowRequest`) | `200 OK` / `204 No Content` |
| **4.2** | `/v2/orgs/{org}/apps/{app}/workflows/{id}/resume` | `POST` | `resume` (`ResumeWorkflowRequest`) | `200 OK` / `204 No Content` |
| **4.3** | `/v2/orgs/{org}/apps/{app}/workflows/{id}/restart` | `POST` | `fork_workflow` (`ForkWorkflowRequest`) | `200 OK` / `201 Created` |

---

## Battery 5: Queues & Schedules Operations

Certifies management of asynchronous workflow queues and periodic schedules.

| Check | REST Endpoint | Method | Wire Message Type | Expected Status |
| :--- | :--- | :---: | :--- | :---: |
| **5.1** | `/v2/orgs/{org}/apps/{app}/queues` | `GET` | `list_queues` | `200 OK` |
| **5.2** | `/v2/orgs/{org}/apps/{app}/schedules` | `GET` | `list_schedules` | `200 OK` |
| **5.3** | `/v2/orgs/{org}/apps/{app}/schedules/{id}/pause` | `POST` | `pause_schedule` | `200 OK` / `204 No Content` |
| **5.4** | `/v2/orgs/{org}/apps/{app}/schedules/{id}/resume` | `POST` | `resume_schedule` | `200 OK` / `204 No Content` |

---

## Battery 6: Workflow Recovery & Liveness Lifecycle

Certifies executor lifecycle transitions and recovery failover dispatch.

| Check | Scenario | Contract Assertions |
| :--- | :--- | :--- |
| **6.1** | Hard Disconnect | Socket closure or missed pings transitions executor from `HEALTHY` to `DISCONNECTED`. |
| **6.2** | Recovery Adoption | When replacement executor connects, Relay dispatches `recovery` frame containing dead executor IDs. Replacement acknowledges with `success: true`. |

---

## Battery 7: Alerting Rules Management

Certifies storing, listing, and removing application alerting rules.

| Check | REST Endpoint | Method | Expected Status | Assertions |
| :--- | :--- | :---: | :---: | :--- |
| **7.1** | `/v2/orgs/{org}/apps/{app}/alerting-rules` | `POST` | `200 OK` / `201 Created` | Persists rule and returns generated `ruleId`. |
| **7.2** | `/v2/orgs/{org}/apps/{app}/alerting-rules` | `GET` | `200 OK` | Returns array containing active alerting rules. |
| **7.3** | `/v2/orgs/{org}/apps/{app}/alerting-rules/{id}` | `DELETE` | `200 OK` / `204 No Content` | Removes alerting rule. |

---

## Battery 8: RFC 9457 Problem Details & Identity Gating

Certifies error response formatting and self-hosted route gating behavior.

| Check | Request | Expected Status | Content-Type | Assertions |
| :--- | :--- | :---: | :--- | :--- |
| **8.1** | Malformed JSON Body | `400 Bad Request` | `application/problem+json` | Problem Details object with `type`, `title`, `status: 400`, and `detail`. |
| **8.2** | Non-Existent Resource | `404 Not Found` | `application/problem+json` | Problem Details object with `status: 404`. |
| **8.3** | Unauthenticated OAuth Endpoint (`/v2/users/me`) | `404 Not Found` | `application/problem+json` | In self-hosted no-auth mode, returns 404 Problem Details rather than 403 Forbidden. |
