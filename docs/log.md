# Documentation update log

This log tracks the evolution of the knowledge base: page additions,
deprecations, and structural refactors. It does not track software releases or
implementation milestones; those belong in the commit history.

## 2026-09-08

* **Creation**: Implemented the automated DBOS Conductor conformance test suite in `internal/conformance`, providing in-process and blackbox test runners across 8 specification batteries, the `relay test-conformance` CLI command, the `make test/conformance` target, and documentation in [conformance testing](usage/conformance.md).
* **Creation**: Implemented the Phase 7 identity and access control layer featuring RSA OIDC JWT token validation with in-memory JWKS caching and fail-closed key set guarantees, RFC 8628 device flow login compatibility, Postgres persistence for users, roles, organisation members, domain claims, and audit logs, automatic user registration and domain-claim organisation matching, conditional route gating returning 404 Problem Details when running in unauthenticated mode, and an integration test suite in `tests/identity/`.
* **Creation**: Implemented the Phase 6 web dashboard in `console/` and `internal/dashboard`, featuring an OpenAPI-aligned TypeScript data layer, fleet and application overview, workflow search with interactive SVG step execution graph, queue and schedule controls, alerting rule manager, and scoped API key minting.
* **Update**: Documented web dashboard access in the [quickstart guide](usage/quickstart.md).
* **Creation**: Implemented the Phase 5 scale and operational architecture featuring high-availability instance clustering with lease adoption, HMAC-SHA256 signed HTTP peer forwarding with loop prevention, an OpenMetrics scrape endpoint at `/v1/metrics`, alerting rule management REST API, background alert evaluation loop dispatching WebSocket notifications to executors, and a multi-node integration test suite.
* **Update**: Documented alerting WebSocket dispatch and multi-instance peer forwarding in [Executor WebSocket Protocol](protocol/executor-ws.md).
* **Creation**: Implemented the Phase 4 workflow recovery engine featuring an isolated pure lifecycle state machine, deterministic virtual timer clock, version-preferring recovery dispatcher with strict tenant isolation, connection hub integration, and an end-to-end chaos test suite.
* **Update**: Documented executor lifecycle state transitions, timeout parameters, and recovery failover mechanics in [Executor WebSocket Protocol](protocol/executor-ws.md).
* **Creation**: Implemented the Phase 3 REST API surface with typed OpenAPI 3.0 code-generation, router-mediated executor dispatch, application metadata and token persistence in Postgres, and conformance test suite for upstream `dbosctl` compatibility.
* **Update**: Updated the [quickstart guide](usage/quickstart.md) with REST API querying, `dbosctl` CLI usage, and RFC 9457 Problem Details error responses.
* **Update**: Expanded [Wire to REST Mapping](protocol/wire-to-rest.md) with comprehensive field and type mappings for workflows, steps, events, notifications, queues, and schedules.
* **Creation**: Implemented the Phase 2 protocol codec, connection hub, and executor endpoints with sample applications in Python and TypeScript.
* **Refactor**: Consolidated OpenAPI specification access and YAML conversion
  in `api/spec`, eliminated redundant schema decoding across packages, and
  sanitized internal verification identifiers across discovery documents.
* **Creation**: Opened the [usage](usage/index.md) section with
  [Quickstart](usage/quickstart.md) for running Relay locally with Postgres.
* **Update**: Added development targets to the [contributor guide](contribution/guide.md)
  and noted completion of discovery items D1-D8 and Phase 1 foundation packages.
* **Update**: Added governance invariants to the [contributor guide](contribution/guide.md)
  and [maintainer guide](contribution/maintainers.md) on scope tightness,
  avoiding duplication and divergence, and keeping committed files free of
  internal process references.
* **Update**: Linked the usage section from the root [index](index.md).
* **Creation**: Opened the [discovery](discovery/index.md) section and its
  [provenance ledger](discovery/provenance.md) recording permitted public
  sources for clean-room derivation.
* **Creation**: Opened the [protocol](protocol/index.md) section index for the
  executor WebSocket specification and wire mappings.
* **Creation**: Added [ADR 0003](adr/0003-implementation-language.md)
  recording Go as the implementation language, which closes the stack question
  discovery item D1 was opened to answer. The record also fixes the supporting
  library choices, including the corrected import path for the WebSocket
  library, whose old path appears in older design notes.
* **Update**: Linked both discovery and protocol sections from the root
  [index](index.md).
* **Update**: [Components](architecture/components.md) now names the planned
  Go package for each module. The page still describes an intended design, and
  says so.

## 2026-09-07

* **Creation**: Opened the bundle with the operational baseline. Added the
  root index, this log, the [overview](overview.md), the design section
  ([goals](design/goals.md), [compatibility
  tiers](design/compatibility-tiers.md), [terminology](design/terminology.md)),
  the architecture section ([components](architecture/components.md),
  [recovery](architecture/recovery.md)), the first two decision records, and
  the contribution section ([guide](contribution/guide.md), [clean-room
  rules](contribution/clean-room.md), [maintainer
  guide](contribution/maintainers.md)).
* **Note**: The bundle deliberately has no usage or reference section yet.
  There is nothing to run, so a page describing how to run it would be
  fiction. Both sections open when the first binary does.
* **Note**: Every architecture page describes an intended design, not shipped
  behaviour, and says so. Pages are rewritten against the code as the code
  lands.

## 2026-09-08, Discovery and Foundation Core

Added discovery reference documents and initial foundation packages:
* Added [REST surface](discovery/D3-rest-surface.md): 64 operations inventoried,
  OIDC-gated routes identified, and OpenAPI specs vendored in `api/spec/`.
* Added [Authorization](discovery/D4-authz.md): permission catalog, role
  model, and API key format.
* Added [Client behaviour](discovery/D5-client-behaviour.md): command reference
  and conformance checklist for `dbosctl`.
* Added [Executor WebSocket protocol](protocol/executor-ws.md) and
  [Wire to REST mapping](protocol/wire-to-rest.md): protocol messages, liveness,
  and recovery guarantees.
* Seeded [provenance ledger](discovery/provenance.md) with confirmed public sources.
* Added [Dashboard reuse](discovery/D6-dashboard-reuse.md): UI component assessment
  and No-Go decision on `@dbos-argus/ui`.
* Added [Recovery timing](discovery/D7-recovery-params.md): heartbeat intervals,
  executor grace periods, and per-app configuration overrides.
* Added [Metrics and alerting](discovery/D8-metrics-alerting.md): OpenMetrics
  catalogue, alerting schemas, and HA peer forwarding conventions.
