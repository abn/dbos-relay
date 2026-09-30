---
type: Reference
---

# Provenance ledger

Register of permitted public sources for Relay protocol and API specifications.
Fact-level citations are tracked in individual discovery documents.
No claim is derived from proprietary binaries or non-public materials.

| Fact | Where used | Source | Date confirmed |
| --- | --- | --- | --- |
| DBOS Transact Go SDK source | Wire protocol, types | https://github.com/dbos-inc/dbos-transact-golang commit fb3e33e0b4c3c709b9271eb935adce5eaf9386f5 (MIT) | 2026-09-30 |
| DBOS Transact Python SDK source | Wire protocol, recovery | https://github.com/dbos-inc/dbos-transact-py commit 2b93e1467a5464f817ef5d11aa5f10d3d2253761 (MIT) | 2026-09-30 |
| DBOS Transact TypeScript SDK source | Wire protocol, schema | https://github.com/dbos-inc/dbos-transact-ts commit 749a4d420127e97715bf1f5d8caabf496a40af0b (MIT) | 2026-09-30 |
| DBOS Transact Java SDK source | Wire protocol, union | https://github.com/dbos-inc/dbos-transact-java commit ecc2bda4deb57e3ba38c55cca150e95c99eb9d64 (MIT) | 2026-09-30 |
| Conductor OpenAPI 3.1 specification | HTTP API surface, schemas | https://cloud.dbos.dev/conductor/v2/openapi.json (SHA256: 61845a5cb182fd63bba7d431a4b296e0d35b667be4c2130845edb53a0e729312) | 2026-09-30 |
| Conductor OpenAPI 3.0 specification | HTTP API surface, schemas | https://cloud.dbos.dev/conductor/v2/openapi-3.0.json (SHA256: 9310bfc55b15d285ed41114cf62b58b53c77f1a225780de0e7dfff74362fc167) | 2026-09-30 |
| dbosctl client source & license | CLI behavior, REST endpoints, MIT license | https://github.com/dbos-inc/dbos-ctl commit 1e6d20e3201588ff67d6157f3808c933b3ed5811 (MIT) | 2026-09-30 |
| DBOS public documentation | Architecture, recovery semantics | https://docs.dbos.dev/ | 2026-09-08 |
| Argus dashboard UI package | Dashboard components | https://github.com/tmarkovski/dbos-argus commit 66ae6da2a7679dfaf8c332cbec8ffa5d31af4314 (MIT) | 2026-09-30 |
| API key format & prefix | Key generation, lookup, and hash verification | dbosctl auth, dbos-transact-ts conductor client | 2026-09-08 |
| Unauthenticated public OpenAPI access | API spec serving | HTTP 200 without auth or license click-through from cloud.dbos.dev | 2026-09-30 |
| Workflow cancellation & resume SQL semantics | Architecture dataplane | https://github.com/dbos-inc/dbos-transact-golang commit fb3e33e0b4c3c709b9271eb935adce5eaf9386f5 (dbos/internal/sysdb/system_database.go) | 2026-09-30 |
