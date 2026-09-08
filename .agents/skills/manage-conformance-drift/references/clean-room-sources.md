# Clean-Room Sources Reference

Canonical listing of permitted clean-room sources and citation standards for Relay
protocol and API development.

---

## 1. Permitted Public Sources

All protocol types, endpoint signatures, behavior models, and test assertions in
Relay are derived strictly from public, open-source repositories and documentation:

### Upstream Open-Source SDKs

| Language | Repository | Canonical Commit | Relevant Protocol Files |
| :--- | :--- | :--- | :--- |
| **Go** | `dbos-inc/dbos-transact-go` | `ab56911fdd78552e1e7fe648cff7c831a1e760c8` | `dbos/conductor_protocol.go`, `dbos/conductor.go` |
| **Python** | `dbos-inc/dbos-transact-py` | `833794f7a1138bacf75ff6d88647a33eb5e35e52` | `dbos/_conductor/conductor.py` |
| **TypeScript**| `dbos-inc/dbos-transact-ts` | `d8c4974cca6cc84b296f3b8edfbbb41627ddd47e` | `src/conductor/conductor.ts` |
| **Java** | `dbos-inc/dbos-transact-java`| `1248174f393bd97f9973ec83cbc6e42b6e319ed1` | `transact/src/main/java/dev/dbos/transact/conductor/Conductor.java` |

### Vendored OpenAPI Specifications

Vendored under `api/spec/` with SHA-256 integrity verification:

- `api/spec/openapi.json`: OpenAPI 3.1.0 specification fetched from
  `https://cloud.dbos.dev/conductor/v2/openapi.json`
- `api/spec/openapi-3.0.json`: OpenAPI 3.0.3 specification fetched from
  `https://cloud.dbos.dev/conductor/v2/openapi-3.0.json`

### Public Documentation

- Documentation portal: `https://docs.dbos.dev`
- Workflow recovery: `https://docs.dbos.dev/production/workflow-recovery`
- Observability: `https://docs.dbos.dev/production/observability`

---

## 2. Forbidden Sources

Under the clean-room rules (`docs/contribution/clean-room.md`), contributors and
agents must **never**:

- Download, run, or observe the proprietary DBOS Conductor container images.
- Inspect the proprietary DBOS Cloud Console frontend bundle or backend services.
- Reverse-engineer network traffic between a DBOS application and DBOS Cloud.
- Benchmark or probe proprietary cloud instances.

If a fact cannot be proven from permitted public SDKs or specifications, do not
guess. Write an isolated test against an unmodified SDK client to determine the
behavior.

---

## 3. Citation Standards in Code

Every protocol message struct in `internal/protocol/messages.go` must carry a
provenance comment directly above its type definition:

```go
// ListWorkflowsResponseBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListWorkflowsResponseBody struct {
    ...
}
```

When adding or updating endpoints in `internal/api/`, cite the owning OpenAPI
operation ID and path:

```go
// GET /v2/orgs/{orgName}/apps/{appName}/workflows (operation: listWorkflows)
```
