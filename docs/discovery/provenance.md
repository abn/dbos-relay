---
type: Reference
---

# Provenance ledger

Register of permitted public sources for Relay protocol and API specifications.
Fact-level citations are tracked in individual discovery documents.
No claim is derived from proprietary binaries or non-public materials.

| Fact | Where used | Source | Date confirmed |
| --- | --- | --- | --- |
| DBOS Transact Go SDK source | Wire protocol, types | https://github.com/dbos-inc/dbos-transact-go commit ab56911fdd78552e1e7fe648cff7c831a1e760c8 (MIT) | 2026-09-08 |
| DBOS Transact Python SDK source | Wire protocol, recovery | https://github.com/dbos-inc/dbos-transact-py commit 833794f7a1138bacf75ff6d88647a33eb5e35e52 (MIT) | 2026-09-08 |
| DBOS Transact TypeScript SDK source | Wire protocol, schema | https://github.com/dbos-inc/dbos-transact-ts commit d8c4974cca6cc84b296f3b8edfbbb41627ddd47e (MIT) | 2026-09-08 |
| DBOS Transact Java SDK source | Wire protocol, union | https://github.com/dbos-inc/dbos-transact-java commit 1248174f393bd97f9973ec83cbc6e42b6e319ed1 (MIT) | 2026-09-08 |
| Conductor OpenAPI 3.1 & 3.0 specification | HTTP API surface, schemas | https://cloud.dbos.dev/conductor/v2/openapi.json (SHA256: b5dc31eb29686a84fe0390a7446b5acdbc0dd05846cc94a746de649b92880722) | 2026-09-08 |
| dbosctl client source & license | CLI behavior, REST endpoints, MIT license | https://github.com/dbos-inc/dbos-ctl commit 9d14ed3f0ccddb84cd3390e0bddbcfb9ea9a32a6 (MIT) | 2026-09-08 |
| DBOS public documentation | Architecture, recovery semantics | https://docs.dbos.dev/ | 2026-09-08 |
| Argus dashboard UI package | Dashboard components | https://github.com/tmarkovski/dbos-argus commit 53cf15bdbea0b68f8ac2dd1e593539e864c08788 (MIT) | 2026-09-08 |
| API key format & prefix | Key generation, lookup, and hash verification | dbosctl auth, dbos-transact-ts conductor client | 2026-09-08 |
| Unauthenticated public OpenAPI access | API spec serving | HTTP 200 without auth or license click-through from cloud.dbos.dev | 2026-09-08 |
