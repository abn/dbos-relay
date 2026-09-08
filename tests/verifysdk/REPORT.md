# Multi-SDK Verification Report

**Date**: 2026-09-08 22:01:56 CEST
**Total Duration**: 25.952s

## Container Inventory and Handshake

| Language | Service Name | Application | Executor ID | Version | Status |
|---|---|---|---|---|---|
| Go | `deploy-app-golang-victim-1` | `golang-sample-app` | `b400c9cf-dc90-4a22-9ba5-47bb0fddef8f` | `61b2f971f515022ccb8264af00bff2b057ad1fbae38e4860de02fa8fefb27dd3` | Active |
| Python | `deploy-app-python-1` | `python-sample-app` | `exec-python-945bb98e` | `v1.0.0` | Active |
| TypeScript | `deploy-app-typescript-1` | `typescript-sample-app` | `exec-ts-d141e8de` | `v1.0.0` | Active |
| Java | `deploy-app-java-1` | `java-sample-app` | `exec-java-3a74ab97` | `v1.0.0` | Active |

## Cell Verification Summary

| Cell | Python | TypeScript | Go | Java | Duration |
|---|---|---|---|---|---|
| 1. Socket connection and presence | PASS | PASS | PASS | PASS | 4ms |
| 2. Conformance and CLI probes | PASS | PASS | PASS | PASS | 25ms |
| 3. Data plane read and preservation | PASS | PASS | PASS | [SKIPPED: scope] | 15ms |
| 4. Field parity between socket and database | PASS | PASS | PASS | [SKIPPED: scope] | 17ms |
| 5. Chaos, real timers, and recovery | PASS | PASS | PASS | [SKIPPED: scope] | 25.406s |
| 6. Offline data-plane cancel and resume | PASS | PASS | PASS | [SKIPPED: scope] | 33ms |
| 7. Data-plane fork to live version | PASS | PASS | PASS | [SKIPPED: scope] | 20ms |

## Verification Invariants Audit

- **SDK Isolation**: Passed `make lint/sdk-isolation`. Zero imports of `internal/fakeexecutor`, `internal/clock`, or mock packages.
- **Real External Processes**: All 4 SDK runtimes executed in real containers under Podman compose (`deploy/compose-sdk-apps.yaml`).
- **Chaos Recovery Assertions**: Harness proved SIGKILL timestamp, DISCONNECTED to DEAD transition, survivor log receipt, and exactly-once workflow outcome.
- **Credential Redaction**: Conductor keys in API queries and container startup logs were redacted as `dbos_sec_***`.
