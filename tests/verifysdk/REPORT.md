# Multi-SDK Verification Report

**Date**: 2026-09-08 22:59:09 CEST
**Total Duration**: 1m47.955s

## Container Inventory and Handshake

| Language | Service Name | Application | Executor ID | Version | Status |
|---|---|---|---|---|---|
| Go | `deploy-app-golang-victim-1` | `golang-sample-app` | `d30d3c12-c1e5-4efa-bd56-bcaeb10e1a25` | `e6210d417c8c4fbc22bee726bf9ba3ec2cbac86f113d732805da3c7ea68a7bc4` | Active |
| Python | `deploy-app-python-victim-1` | `python-sample-app` | `da7c8eca-2c19-483d-bf4c-30cc72941d54` | `bed88df4c423eb72008e1efb545d6f00` | Active |
| TypeScript | `deploy-app-typescript-victim-1` | `typescript-sample-app` | `90a424e5-7be3-4fea-81ff-0a8147e477f8` | `49a7af078c729a5461a688b8abd6a9bc` | Active |
| Java | `deploy-app-java-1` | `java-sample-app` | `17e0bb22-d3cb-45a0-8e13-a1749e2e77f7` | `a888de2451d5e444aaf752c53a65bf16727d797417f8f0a9851f0d1cb13bd8cb` | Active |

## Cell Verification Summary

| Cell | Python | TypeScript | Go | Java | Duration |
|---|---|---|---|---|---|
| 1. Socket connection and presence | PASS | PASS | PASS | PASS | 68ms |
| 2. Conformance and CLI probes | PASS | PASS | PASS | PASS | 24ms |
| 3. Data plane read and preservation | PASS | PASS | PASS | [SKIPPED: scope] | 6ms |
| 4. Field parity between socket and database | PASS | PASS | PASS | [SKIPPED: scope] | 7ms |
| 5. Chaos, real timers, and recovery | PASS | PASS | PASS | [SKIPPED: scope] | 1m47.507s |
| 6. Offline data-plane cancel and resume | PASS | PASS | PASS | [SKIPPED: scope] | 46ms |
| 7. Data-plane fork to live version | PASS | PASS | PASS | [SKIPPED: scope] | 9ms |

## Verification Invariants Audit

- **SDK Isolation**: Passed `make lint/sdk-isolation` and `make lint/examples-isolation`. Zero imports of fake/mock protocol code or internal packages in examples.
- **Real External Processes**: All 4 SDK runtimes executed in real containers under Podman compose (`deploy/compose-sdk-apps.yaml`) using official SDK packages (`dbos` PyPI, `@dbos-inc/dbos-sdk` npm, `dev.dbos:transact` Maven Central, `github.com/dbos-inc/dbos-transact-golang`).
- **Chaos Recovery Assertions**: Harness proved kill timestamp, DISCONNECTED to DEAD transition, survivor log receipt, and step outcome counts.
- **Credential Redaction**: Conductor keys in API queries and container startup logs were redacted as `dbos_sec_***`.
