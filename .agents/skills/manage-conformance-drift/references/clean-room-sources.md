# Clean-Room Sources Reference

Citation standards for Relay protocol and API development.

---

## 1. Clean-Room Sources

The canonical and only copy of permitted and forbidden sources is maintained in
[Clean-Room Rules](../../../../docs/contribution/clean-room.md) (`docs/contribution/clean-room.md`).
Per-fact commits and repository pins are recorded in the provenance ledger
at `docs/discovery/provenance.md`. Vendored OpenAPI specifications are maintained
under `api/spec/`.

Contributors and agents must never restate the permitted source list. Refer
directly to `docs/contribution/clean-room.md` and `docs/discovery/provenance.md`.

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
