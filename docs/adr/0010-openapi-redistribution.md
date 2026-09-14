---
type: Decision
title: ADR 0010 - Redistribution of OpenAPI interface specifications
description: Interface copyright and attribution posture for vendored OpenAPI documents.
status: stable
---

# ADR 0010 - Redistribution of OpenAPI interface specifications

## Status

Accepted

## Context

Relay vendors the public OpenAPI documents for the Conductor HTTP API under `api/spec/` (`openapi.json` and `openapi-3.0.json`). These documents serve as the contract for automated server-side stub generation (`internal/api/gen/api.gen.go`) and are served via `/openapi.json`, `/openapi-3.0.json`, and `/openapi.yaml` for client tool introspection.

The clean-room rules (`docs/contribution/clean-room.md`) permit reading publicly served OpenAPI documents fetched without authentication. Embedding and redistributing these documents within binary releases and source distributions requires a clear legal and architectural posture:

1. **Functional interface description**: The OpenAPI documents describe the public functional interface and wire contract necessary for interoperability. Under copyright law and clean-room principles, functional API interfaces, method names, endpoints, and parameter definitions constitute factual interface definitions rather than protectable expressive works.
2. **Third-party attribution**: The upstream documents are third-party materials. They are not authored by Relay contributors and are therefore excluded from Relay's MIT license grant. A dedicated `NOTICE` file in the repository root explicitly identifies these files and their provenance.
3. **Redistribution rights**: Because the specifications are published freely and openly without click-through terms, authentication barriers, or restrictive copyright notices on public endpoints, distributing them unmodified for interoperability and protocol conformance conforms with fair clean-room development practices.

## Decision

We record that:
1. Relay vendors the Conductor OpenAPI specifications under `api/spec/` solely for interface conformance and code generation.
2. These files are documented in `NOTICE` and `api/spec/PROVENANCE.md` as third-party material excluded from Relay's MIT copyright grant.
3. Relay will continue to serve these specifications at `/openapi.json`, `/openapi-3.0.json`, and `/openapi.yaml` to enable unmodified DBOS clients to validate API schemas against the control plane.
4. Independent legal review prior to first public release (as established in ADR 0001) will re-verify the redistribution posture of embedded interface descriptions.

## Consequences

- Third-party material is clearly attributed and differentiated from Relay's MIT-licensed codebase.
- No application or client changes are required; standard SDKs can continue to consume `/openapi.json`.
- The provenance and redistribution rationale are publicly documented and linked from the project's architecture records.
