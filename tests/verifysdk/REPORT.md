# Multi-SDK Verification Report

**Date**: 2026-09-09 00:06:53 CEST
**Total Duration**: 2m50.369s

## Container Inventory and Handshake

| Language | Primary Service | Secondary Service | Application | Primary Executor ID | Version | Status |
|---|---|---|---|---|---|---|
| Go | `deploy-app-golang-primary-1` | `deploy-app-golang-secondary-1` | `golang-sample-app` | `a7912967-37ee-4028-af59-936fc1b63c4d` | `f3f65019d6068070517ea39acc8625b17dde9fe81eb7e12a7428809c5c94a441` | Active |
| Python | `deploy-app-python-primary-1` | `deploy-app-python-secondary-1` | `python-sample-app` | `e126de83-3fba-4478-a00c-dc471cce6e20` | `d473fcaa9aa2f11e8421a1f3463e13ec` | Active |
| TypeScript | `deploy-app-typescript-primary-1` | `deploy-app-typescript-secondary-1` | `typescript-sample-app` | `eef9a2f1-8ad0-4165-a2e4-2921056cf595` | `d6f7a5101106ec316a9cb86a419dbb40` | Active |
| Java | `deploy-app-java-1` | `none` | `java-sample-app` | `0e2851c6-c767-4e6d-aa4d-6d0940a06620` | `a888de2451d5e444aaf752c53a65bf16727d797417f8f0a9851f0d1cb13bd8cb` | Active |

## Cell Verification Summary

| Cell | Python | TypeScript | Go | Java | Duration |
|---|---|---|---|---|---|
| 1. Socket connection and presence | PASS | PASS | PASS | PASS | 90ms |
| 2. Conformance and CLI suite | PASS | PASS | PASS | PASS | 5.064s |
| 3. Data plane read and preservation | PASS | PASS | PASS | [SKIPPED: scope] | 7ms |
| 4. Field parity between socket and database | PASS | PASS | PASS | [SKIPPED: scope] | 27ms |
| 5. Chaos, real timers, and recovery | PASS | PASS | PASS | [SKIPPED: scope] | 1m33.652s |
| 6. Offline data-plane cancel and resume | PASS | PASS | PASS | [SKIPPED: scope] | 1m9.211s |
| 7. Data-plane fork to live version | PASS | PASS | PASS | [SKIPPED: scope] | 2.075s |

## Cell 2 Conformance and D5 REST Scorecard

| Language | Conformance Suite Summary | D5 dbosctl REST Battery Summary |
|---|---|---|
| Go | 8/8 batteries conformant against control plane and live SDK | 11/11 D5 REST endpoints verified conformant |
| Python | 8/8 batteries conformant against control plane and live SDK | 11/11 D5 REST endpoints verified conformant |
| TypeScript | 8/8 batteries conformant against control plane and live SDK | 11/11 D5 REST endpoints verified conformant |
| Java | 4/8 batteries passed (Battery 1 Spec, Battery 2 Handshake, Battery 3 Observability, Battery 8 Problem Details; Skipped by Java scope: Battery 4 Control, Battery 5 Queues/Schedules, Battery 6 Recovery, Battery 7 Alerting) | 10/11 D5 REST endpoints verified conformant |

## Mid-Run Container Inventory (podman ps)

```
NAMES                              STATUS                  PORTS
tailscale-serve-proxy              Up 14 hours
nowledge-mem-server                Up 14 hours             14242/tcp
caddy                              Up 14 hours             80/tcp, 443/tcp, 2019/tcp, 443/udp
phoenix                            Up 14 hours (healthy)   0.0.0.0:6006->6006/tcp, 4317/tcp, 9090/tcp
aio-sandbox-nmem                   Up 14 hours (healthy)   127.0.0.1:28081->8080/tcp
hidiee-postgres                    Up 11 hours (healthy)   0.0.0.0:5432->5432/tcp
deploy-relay-1                     Up 35 minutes           0.0.0.0:8090->8090/tcp
deploy-postgres-1                  Up 8 minutes (healthy)  0.0.0.0:5433->5432/tcp
deploy-app-golang-secondary-1      Up 2 minutes            0.0.0.0:8086->8086/tcp
deploy-app-golang-primary-1        Up 2 minutes            0.0.0.0:8080->8080/tcp
deploy-app-java-1                  Up 8 minutes            0.0.0.0:8083->8083/tcp
deploy-app-python-secondary-1      Up 2 minutes            0.0.0.0:8084->8084/tcp
deploy-app-python-primary-1        Up 2 minutes            0.0.0.0:8081->8081/tcp
deploy-app-typescript-secondary-1  Up 2 minutes            0.0.0.0:8085->8085/tcp
deploy-app-typescript-primary-1    Up 2 minutes            0.0.0.0:8082->8082/tcp
```

## Per-Language Verification Evidence

### Go Runtime Evidence

- **Application**: `golang-sample-app`
- **Launch Log Excerpt**: `time=2026-09-08T21:55:54.331Z level=INFO msg="Initializing DBOS context" app_name=golang-sample-app dbos_version=v1.3.0`
- **Cell 5 Kill Timestamp**: `2026-09-09T00:04:16+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-09T00:04:17+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-09T00:04:28+02:00`
- **Cell 5 DEAD - Kill Delta**: `12s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `Secondary executor active and processing recovery dispatch`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Duration**: `19.704s` (honoured cancel and resume across container restart)
- **Cell 7 Duration**: `526ms` (forked workflow `b3b22b55-3f4e-4ec1-b20b-e7f3f780c443` executed to SUCCESS by live executor `fea9d487-29a9-4290-affb-a9cb95867b2e`)

### Python Runtime Evidence

- **Application**: `python-sample-app`
- **Launch Log Excerpt**: `21:55:58 [    INFO] (dbos:_dbos.py:500) Initializing DBOS (v2.31.1)`
- **Cell 5 Kill Timestamp**: `2026-09-09T00:04:44+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-09T00:04:45+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-09T00:04:56+02:00`
- **Cell 5 DEAD - Kill Delta**: `12s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `22:04:55 [    INFO] (dbos:_recovery.py:69) Recovering 2 workflows for executor 722c2bc7-1862-433d-a959-739ac790483f from version d473fcaa9aa2f11e8421a1f3463e13ec`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Duration**: `24.779s` (honoured cancel and resume across container restart)
- **Cell 7 Duration**: `533ms` (forked workflow `1cc4ed9f-3df4-4cec-94a6-c8114c3f4d6a` executed to SUCCESS by live executor `1e9b0dce-339d-49b9-b02a-339c490007ef`)

### TypeScript Runtime Evidence

- **Application**: `typescript-sample-app`
- **Launch Log Excerpt**: `Initializing DBOS (v4.27.6)`
- **Cell 5 Kill Timestamp**: `2026-09-09T00:05:13+02:00`
- **Cell 5 Disconnected Timestamp**: `2026-09-09T00:05:14+02:00`
- **Cell 5 Dead Timestamp**: `2026-09-09T00:05:25+02:00`
- **Cell 5 DEAD - Kill Delta**: `12s` (asserted >= 10s grace period)
- **Secondary Container Recovery Log Line**: `Recovering 2 workflows from application version d6f7a5101106ec316a9cb86a419dbb40`
- **Terminal Outcome Count**: `1` (exactly 1)
- **Step Re-executions**: `0`
- **Cell 6 Duration**: `24.726s` (honoured cancel and resume across container restart)
- **Cell 7 Duration**: `1.015s` (forked workflow `e63dacd4-e455-40d9-85fe-dafa1f75de0f` executed to SUCCESS by live executor `2ed0e3a2-ab12-4f5e-8657-a444ce500470`)

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
