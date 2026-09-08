# Multi-SDK Verification Report

**Date**: 2026-09-08 20:58:24 CEST
**Total Duration**: 25.796s

## Container Inventory and Handshake

| Language | Service Name | Application | Executor ID | Version | Status |
|---|---|---|---|---|---|
| Go | `deploy-app-golang-victim-1` | `golang-sample-app` | `cb8498e3-26a0-4198-a7f1-2650f9e51a2f` | `d2f0dd63a3daa063e1088035853939d1f922073b77dc5a7c9f881181ac0092d8` | Active |
| Python | `deploy-app-python-1` | `python-sample-app` | `exec-python-ea8f17ed` | `v1.0.0` | Active |
| TypeScript | `deploy-app-typescript-1` | `typescript-sample-app` | `exec-ts-339f14b7` | `v1.0.0` | Active |
| Java | `deploy-app-java-1` | `java-sample-app` | `exec-java-a29814c0` | `v1.0.0` | Active |

## Cell Verification Summary

| Cell | Python | TypeScript | Go | Java | Duration |
|---|---|---|---|---|---|
| 1. Socket connection and presence | PASS | PASS | PASS | PASS | 4ms |
| 2. Conformance and CLI probes | PASS | PASS | PASS | PASS | 51ms |
| 3. Data plane read and preservation | PASS | PASS | PASS | [SKIPPED: scope] | 13ms |
| 4. Field parity between socket and database | PASS | PASS | PASS | [SKIPPED: scope] | 13ms |
| 5. Chaos, real timers, and recovery | PASS | PASS | PASS | [SKIPPED: scope] | 25.409s |
| 6. Offline data-plane cancel and resume | PASS | PASS | PASS | [SKIPPED: scope] | 35ms |
| 7. Data-plane fork to live version | PASS | PASS | PASS | [SKIPPED: scope] | 24ms |

## Verification Invariants Audit

- **SDK Isolation**: Passed `make lint/sdk-isolation`. Zero imports of `internal/fakeexecutor`, `internal/clock`, or mock packages.
- **Real External Processes**: All 4 SDK runtimes executed in real containers under Podman compose (`deploy/compose-sdk-apps.yaml`).
- **Chaos Recovery Assertions**: Harness proved SIGKILL timestamp, DISCONNECTED to DEAD transition, survivor log receipt, and exactly-once workflow outcome.
- **Credential Redaction**: Conductor keys in API queries and container startup logs were redacted as `dbos_sec_***`.
