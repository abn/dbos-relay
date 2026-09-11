# Multi-SDK Verification Report

**Date**: 2026-09-12 00:54:49 CEST
**Total Duration**: 2m0.468s

## Container Inventory and Handshake

| Language | Primary Service | Secondary Service | Application | Primary Executor ID | Version | Status |
|---|---|---|---|---|---|---|
| Go | `deploy-app-golang-primary-1` | `deploy-app-golang-secondary-1` | `golang-sample-app` | `7e335d53-798e-406d-9ebd-d8e9ce0faba0` | `01e72e7586c688491dfa541968ba2c19a4eb7179983b71aa7c301428aa337a5e` | Active |
| Python | `deploy-app-python-primary-1` | `deploy-app-python-secondary-1` | `python-sample-app` | `79da85ac-97db-46f1-8091-474b16c9ba48` | `d473fcaa9aa2f11e8421a1f3463e13ec` | Active |
| TypeScript | `deploy-app-typescript-primary-1` | `deploy-app-typescript-secondary-1` | `typescript-sample-app` | `eedf0cd5-e49c-421c-a885-7a6f5a4eb67f` | `d6f7a5101106ec316a9cb86a419dbb40` | Active |
| Java | `deploy-app-java-1` | `deploy-app-java-secondary-1` | `java-sample-app` | `fdf81533-0f87-4591-9234-9427b870d175` | `74addd6612068d96f79e574f061ede6cf9184e09571ba6c1708673517c690f72` | Active |

## Cell Verification Summary

| Cell | Python | TypeScript | Go | Java | Duration |
|---|---|---|---|---|---|
| 1. Socket connection and presence | PASS | PASS | PASS | PASS | 207ms |
| 2. Conformance and CLI suite | PASS | PASS | PASS | PASS | 19.682s |
| 3. Data plane read and preservation | PASS | PASS | PASS | PASS | 13ms |
| 4. Field parity between socket and database | PASS | PASS | PASS | [SKIPPED: upstream schema v19 vs v107] | 80ms |
| 5. Chaos, real timers, and recovery | PASS | PASS | PASS | PASS | 55.977s |
| 6. Offline data-plane cancel and resume | PASS | PASS | PASS | [SKIPPED: upstream schema v19 vs v107] | 41.591s |
| 7. Data-plane fork to live version | PASS | PASS | PASS | PASS | 2.153s |

## Cell 2 Conformance and D5 REST Scorecard

| Language | Conformance Suite Summary | D5 dbosctl REST Battery Summary |
|---|---|---|
| Go | 5/8 batteries passed (B1 (Specification & System Probes), B2 (WebSocket Handshake & Fleet Registration), B6 (Workflow Recovery & Liveness Lifecycle), B7 (Alerting Rules Management), B8 (RFC 9457 Problem Details & Identity Gating)) | 11/11 D5 REST endpoints verified conformant |
| Python | 6/8 batteries passed (B1 (Specification & System Probes), B2 (WebSocket Handshake & Fleet Registration), B4 (Workflow Control Operations), B6 (Workflow Recovery & Liveness Lifecycle), B7 (Alerting Rules Management), B8 (RFC 9457 Problem Details & Identity Gating)) | 11/11 D5 REST endpoints verified conformant |
| TypeScript | 6/8 batteries passed (B1 (Specification & System Probes), B2 (WebSocket Handshake & Fleet Registration), B3 (REST & Wire Multiplexing (Observability)), B6 (Workflow Recovery & Liveness Lifecycle), B7 (Alerting Rules Management), B8 (RFC 9457 Problem Details & Identity Gating)) | 11/11 D5 REST endpoints verified conformant |
| Java | 6/8 batteries passed (B1 (Specification & System Probes), B2 (WebSocket Handshake & Fleet Registration), B4 (Workflow Control Operations), B6 (Workflow Recovery & Liveness Lifecycle), B7 (Alerting Rules Management), B8 (RFC 9457 Problem Details & Identity Gating)) | 10/11 D5 REST endpoints verified conformant |

## Mid-Run Container Inventory (podman ps)

```
NAMES                              STATUS                   PORTS
deploy-postgres-1                  Up 31 hours (healthy)    0.0.0.0:5433->5432/tcp
deploy-relay-1                     Up 49 minutes            0.0.0.0:8090->8090/tcp
deploy-app-golang-primary-1        Up 45 minutes            0.0.0.0:8080->8080/tcp
deploy-app-golang-secondary-1      Up 45 minutes            0.0.0.0:8086->8086/tcp
deploy-app-python-secondary-1      Up 44 minutes            0.0.0.0:8084->8084/tcp
deploy-app-typescript-secondary-1  Up 44 minutes            0.0.0.0:8085->8085/tcp
deploy-app-python-primary-1        Up 44 minutes            0.0.0.0:8081->8081/tcp
deploy-app-typescript-primary-1    Up 44 minutes            0.0.0.0:8082->8082/tcp
deploy-app-java-secondary-1        Up 11 minutes            0.0.0.0:8087->8087/tcp
deploy-app-java-1                  Up 11 minutes            0.0.0.0:8083->8083/tcp
```

## Per-Language Verification Evidence

### Go Runtime Evidence

- **Application**: `golang-sample-app`
- **Launch Log Excerpt**: `time=2026-09-11T22:07:37.800Z level=INFO msg="DBOS launched" app_version=01e72e7586c688491dfa541968ba2c19a4eb7179983b71aa7c301428aa337a5e executor_id=7e335d53-798e-406d-9ebd-d8e9ce0faba0`
- **Cell 5 Kill Timestamp**: `2026-09-12T00:53:10+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-12T00:53:10+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-12T00:53:20+02:00`
- **Cell 5 DEAD - Kill Delta**: `10.14s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `time=2026-09-11T22:53:20.379Z level=INFO msg="Successfully recovered pending workflows" service=conductor executor_ids=[7e335d53-798e-406d-9ebd-d8e9ce0faba0]`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Duration**: `6.771s` (honoured cancel and resume across container restart)
- **Cell 7 Duration**: `510ms` (forked workflow `7e371ba0-5bd3-4eaa-bc5c-b8f8642ae150` executed to SUCCESS by live executor `b555e27a-aada-491d-95a6-e640c4e154eb`)

### Python Runtime Evidence

- **Application**: `python-sample-app`
- **Launch Log Excerpt**: `DBOS Python sample application launched successfully for app python-sample-app`
- **Cell 5 Kill Timestamp**: `2026-09-12T00:53:24+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-12T00:53:24+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-12T00:53:34+02:00`
- **Cell 5 DEAD - Kill Delta**: `10.161s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `22:53:34 [    INFO] (dbos:_recovery.py:69) Recovering 2 workflows for executor 79da85ac-97db-46f1-8091-474b16c9ba48 from version d473fcaa9aa2f11e8421a1f3463e13ec`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Duration**: `16.934s` (honoured cancel and resume across container restart)
- **Cell 7 Duration**: `531ms` (forked workflow `1eb51544-ac5c-4f6a-b058-6624622eb32d` executed to SUCCESS by live executor `7a4d08c1-5522-4b90-9d65-592cde3ac01d`)

### TypeScript Runtime Evidence

- **Application**: `typescript-sample-app`
- **Launch Log Excerpt**: `DBOS TypeScript sample application launched successfully for app typescript-sample-app`
- **Cell 5 Kill Timestamp**: `2026-09-12T00:53:38+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-12T00:53:38+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-12T00:53:48+02:00`
- **Cell 5 DEAD - Kill Delta**: `10.14s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `Recovering 2 workflows from application version d6f7a5101106ec316a9cb86a419dbb40`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Duration**: `17.238s` (honoured cancel and resume across container restart)
- **Cell 7 Duration**: `537ms` (forked workflow `cd67ac81-faa2-4401-a379-d3baa38f1adc` executed to SUCCESS by live executor `7fcda9b6-71cc-487e-9f74-afe59e39add2`)

### Java Runtime Evidence

- **Application**: `java-sample-app`
- **Launch Log Excerpt**: `DBOS Java sample application launched successfully for app java-sample-app`
- **Cell 5 Kill Timestamp**: `2026-09-12T00:53:52+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-12T00:53:52+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-12T00:54:02+02:00`
- **Cell 5 DEAD - Kill Delta**: `10.147s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `[ForkJoinPool.commonPool-worker-2] INFO dev.dbos.transact.conductor.Conductor - Completed processing request: type=recovery, id=e3fa9d1a-986d-4a9c-9e45-79f9bf961fe3, duration=30ms`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Status**: Skipped (upstream DBOS Java SDK 0.8.0 schema version 19 lacks completed_at required by Go SDK data-plane client v107)
- **Cell 7 Duration**: `575ms` (forked workflow `41eee29f-f52b-4a32-af4d-a034abf99b88` executed to SUCCESS by live executor `503330b8-fa2f-4e79-bc39-2c332f9baabd`)

## Verification Invariants Audit

- **SDK Isolation**: Passed `make lint/sdk-isolation` and `make lint/examples-isolation`. Zero imports of fake/mock protocol code or internal packages in examples.
- **Real External Processes**: All 4 SDK runtimes executed in real containers under Podman compose (`deploy/compose-sdk-apps.yaml`) using official SDK packages (`dbos` PyPI, `@dbos-inc/dbos-sdk` npm, `dev.dbos:transact` Maven Central, `github.com/dbos-inc/dbos-transact-golang`).
- **Chaos Recovery Assertions**: Harness proved kill timestamp, DISCONNECTED to DEAD transition with elapsed >= grace, secondary log recovery dispatch, and exactly-once terminal outcome.
- **Offline Cancel & Resume**: Container stopped, data-plane cancel and resume dispatched, container restarted, and restarted executor verified to honor both states.
- **Fork Dequeue & Terminal Execution**: Workflow forked natively via SDK runtime and verified to dequeue and execute to SUCCESS with live executor ID recorded.
- **Credential Redaction**: Conductor keys in API queries and container startup logs were redacted as `dbos_sec_***`.
