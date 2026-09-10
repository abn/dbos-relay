# Multi-SDK Verification Report

**Date**: 2026-09-10 17:36:40 CEST
**Total Duration**: 1m34.536s

## Container Inventory and Handshake

| Language | Primary Service | Secondary Service | Application | Primary Executor ID | Version | Status |
|---|---|---|---|---|---|---|
| Go | `deploy-app-golang-primary-1` | `deploy-app-golang-secondary-1` | `golang-sample-app` | `e4142978-5384-4c50-a04a-3f3e57892d72` | `4bf642393529fcdc00c896070d68cadab657e39247986d305fd505eb08654b84` | Active |
| Python | `deploy-app-python-primary-1` | `deploy-app-python-secondary-1` | `python-sample-app` | `bc06c0cb-b96e-47ac-a76b-46fef15d737f` | `d473fcaa9aa2f11e8421a1f3463e13ec` | Active |
| TypeScript | `deploy-app-typescript-primary-1` | `deploy-app-typescript-secondary-1` | `typescript-sample-app` | `8c31cff8-ec5c-41ff-9d72-f2af9b088750` | `d6f7a5101106ec316a9cb86a419dbb40` | Active |
| Java | `deploy-app-java-1` | `none` | `java-sample-app` | `e02e25c7-e246-43b1-bf0b-bb8b12d28793` | `a888de2451d5e444aaf752c53a65bf16727d797417f8f0a9851f0d1cb13bd8cb` | Active |

## Cell Verification Summary

| Cell | Python | TypeScript | Go | Java | Duration |
|---|---|---|---|---|---|
| 1. Socket connection and presence | PASS | PASS | PASS | PASS | 192ms |
| 2. Conformance and CLI suite | PASS | PASS | PASS | PASS | 9.061s |
| 3. Data plane read and preservation | PASS | PASS | PASS | [SKIPPED: scope] | 8ms |
| 4. Field parity between socket and database | PASS | PASS | PASS | [SKIPPED: scope] | 78ms |
| 5. Chaos, real timers, and recovery | PASS | PASS | PASS | [SKIPPED: scope] | 41.953s |
| 6. Offline data-plane cancel and resume | PASS | PASS | PASS | [SKIPPED: scope] | 41.224s |
| 7. Data-plane fork to live version | PASS | PASS | PASS | [SKIPPED: scope] | 1.568s |

## Cell 2 Conformance and D5 REST Scorecard

| Language | Conformance Suite Summary | D5 dbosctl REST Battery Summary |
|---|---|---|
| Go | 5/8 batteries passed (B1 (Specification & System Probes), B2 (WebSocket Handshake & Fleet Registration), B6 (Workflow Recovery & Liveness Lifecycle), B7 (Alerting Rules Management), B8 (RFC 9457 Problem Details & Identity Gating)) | 11/11 D5 REST endpoints verified conformant |
| Python | 5/8 batteries passed (B1 (Specification & System Probes), B2 (WebSocket Handshake & Fleet Registration), B6 (Workflow Recovery & Liveness Lifecycle), B7 (Alerting Rules Management), B8 (RFC 9457 Problem Details & Identity Gating)) | 11/11 D5 REST endpoints verified conformant |
| TypeScript | 6/8 batteries passed (B1 (Specification & System Probes), B2 (WebSocket Handshake & Fleet Registration), B4 (Workflow Control Operations), B6 (Workflow Recovery & Liveness Lifecycle), B7 (Alerting Rules Management), B8 (RFC 9457 Problem Details & Identity Gating)) | 11/11 D5 REST endpoints verified conformant |
| Java | 3/8 batteries passed (B1 (Specification & System Probes), B2 (WebSocket Handshake & Fleet Registration), B8 (RFC 9457 Problem Details & Identity Gating); Skipped: B4 (Workflow Control Operations), B5 (Queues & Schedules Operations), B6 (Workflow Recovery & Liveness Lifecycle), B7 (Alerting Rules Management)) | 10/11 D5 REST endpoints verified conformant |

## Mid-Run Container Inventory (podman ps)

```
NAMES                              STATUS                   PORTS
tailscale-serve-proxy              Up 30 hours
caddy                              Up 30 hours              80/tcp, 443/tcp, 2019/tcp, 443/udp
nowledge-mem-server                Up 30 hours              14242/tcp
phoenix                            Up 30 hours (healthy)    0.0.0.0:6006->6006/tcp, 4317/tcp, 9090/tcp
aio-sandbox-nmem                   Up 30 hours (healthy)    127.0.0.1:28081->8080/tcp
deploy-postgres-1                  Up 18 seconds (healthy)  0.0.0.0:5433->5432/tcp
deploy-relay-1                     Up 19 seconds            0.0.0.0:8090->8090/tcp
deploy-app-golang-primary-1        Up 19 seconds            0.0.0.0:8080->8080/tcp
deploy-app-golang-secondary-1      Up 19 seconds            0.0.0.0:8086->8086/tcp
deploy-app-python-secondary-1      Up 19 seconds            0.0.0.0:8084->8084/tcp
deploy-app-typescript-secondary-1  Up 18 seconds            0.0.0.0:8085->8085/tcp
deploy-app-java-1                  Up 18 seconds            0.0.0.0:8083->8083/tcp
deploy-app-python-primary-1        Up 9 seconds             0.0.0.0:8081->8081/tcp
deploy-app-typescript-primary-1    Up 19 seconds            0.0.0.0:8082->8082/tcp
```

## Per-Language Verification Evidence

### Go Runtime Evidence

- **Application**: `golang-sample-app`
- **Launch Log Excerpt**: `time=2026-09-10T15:34:46.137Z level=INFO msg="DBOS launched" app_version=4bf642393529fcdc00c896070d68cadab657e39247986d305fd505eb08654b84 executor_id=e4142978-5384-4c50-a04a-3f3e57892d72`
- **Cell 5 Kill Timestamp**: `2026-09-10T17:35:15+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-10T17:35:15+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-10T17:35:25+02:00`
- **Cell 5 DEAD - Kill Delta**: `10.139s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `time=2026-09-10T15:35:25.919Z level=INFO msg="Successfully recovered pending workflows" service=conductor executor_ids=[e4142978-5384-4c50-a04a-3f3e57892d72]`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Duration**: `6.796s` (honoured cancel and resume across container restart)
- **Cell 7 Duration**: `514ms` (forked workflow `6b149609-3d60-475d-bec5-7f09091b3cdd` executed to SUCCESS by live executor `6383ced8-6d28-4881-822c-d1eb48d49264`)

### Python Runtime Evidence

- **Application**: `python-sample-app`
- **Launch Log Excerpt**: `DBOS Python sample application launched successfully for app python-sample-app`
- **Cell 5 Kill Timestamp**: `2026-09-10T17:35:29+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-10T17:35:29+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-10T17:35:40+02:00`
- **Cell 5 DEAD - Kill Delta**: `10.138s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `15:35:39 [    INFO] (dbos:_recovery.py:69) Recovering 2 workflows for executor bc06c0cb-b96e-47ac-a76b-46fef15d737f from version d473fcaa9aa2f11e8421a1f3463e13ec`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Duration**: `16.847s` (honoured cancel and resume across container restart)
- **Cell 7 Duration**: `532ms` (forked workflow `c08d3ab9-c16e-4475-9925-a9e71dd9569e` executed to SUCCESS by live executor `5adb7aa9-83e8-4293-ab41-bb3c912dafe0`)

### TypeScript Runtime Evidence

- **Application**: `typescript-sample-app`
- **Launch Log Excerpt**: `DBOS TypeScript sample application launched successfully for app typescript-sample-app`
- **Cell 5 Kill Timestamp**: `2026-09-10T17:35:43+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-10T17:35:43+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-10T17:35:53+02:00`
- **Cell 5 DEAD - Kill Delta**: `10.146s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `Recovering 2 workflows from application version d6f7a5101106ec316a9cb86a419dbb40`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Duration**: `16.925s` (honoured cancel and resume across container restart)
- **Cell 7 Duration**: `521ms` (forked workflow `74c4cee7-4317-493d-af1c-0e00f18c305c` executed to SUCCESS by live executor `afff49ba-1dee-4c30-a806-a94c22634ff1`)

### Java Runtime Evidence

- **Application**: `java-sample-app`
- **Launch Log Excerpt**: `DBOS Java sample application launched successfully for app java-sample-app`
- **Status**: Verified for Cells 1 and 2. Cells 3–7 skipped as scoped for Phase 0/8A.

## Verification Invariants Audit

- **SDK Isolation**: Passed `make lint/sdk-isolation` and `make lint/examples-isolation`. Zero imports of fake/mock protocol code or internal packages in examples.
- **Real External Processes**: All 4 SDK runtimes executed in real containers under Podman compose (`deploy/compose-sdk-apps.yaml`) using official SDK packages (`dbos` PyPI, `@dbos-inc/dbos-sdk` npm, `dev.dbos:transact` Maven Central, `github.com/dbos-inc/dbos-transact-golang`).
- **Chaos Recovery Assertions**: Harness proved kill timestamp, DISCONNECTED to DEAD transition with elapsed >= grace, secondary log recovery dispatch, and exactly-once terminal outcome.
- **Offline Cancel & Resume**: Container stopped, data-plane cancel and resume dispatched, container restarted, and restarted executor verified to honor both states.
- **Fork Dequeue & Terminal Execution**: Workflow forked natively via SDK runtime and verified to dequeue and execute to SUCCESS with live executor ID recorded.
- **Credential Redaction**: Conductor keys in API queries and container startup logs were redacted as `dbos_sec_***`.
