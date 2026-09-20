# Documentation update log

This log tracks the evolution of the knowledge base and architectural capabilities:
page additions, deprecations, and structural refactors.

## Unreleased

* **Update**: Implemented full Conductor metrics parity on `/v1/metrics` via executor aggregate dispatch (`get_workflow_aggregates`, `get_step_aggregates`), covering all workflow and step rate, count, oldest-timestamp, and windowed-max families with clock-aligned minute windows, `applications`/`workflow_names`/`metrics` filters, and `metric.read` permission acceptance. See [Metrics, alerting, and HA peer forwarding](discovery/metrics-alerting.md).

## 2026-09-18

* **Creation**: Added software [Changelog](changelog.md) managed by Release Please in `docs/` and rendered on the documentation site, reserving it in the OKF bundle validator.
* **Update**: Implemented dual-engine storage architecture (ADR 0011) with pure-Go embedded SQLite engine (`modernc.org/sqlite`) alongside flagship PostgreSQL, single-node HA standalone mode with automatic orphan reconciliation, `--embedded` CLI flag, full test coverage, and DBOS Conductor conformance verification across all batteries.
* **Creation**: Added [ADR 0011](adr/0011-dual-engine-storage-architecture.md) establishing dual-engine storage architecture with PostgreSQL as flagship production engine, pure-Go SQLite for zero-dependency embedded mode, Turso MVCC for scale-out, and future adoption of Turso's PostgreSQL wire mode.
* **Update**: Converted ASCII architecture and lifecycle diagrams across docs to Mermaid flowcharts and sequence diagrams with vendored offline runtime, added Chroma syntax highlighting to wiki rendering, made documentation sidebar sections collapsible, and unified section headings with section index routes.
* **Update**: Documented multi-database fleet architecture and design rationale for omitting database credentials and topology management from the control plane UI in [Data plane access via SDK client](architecture/dataplane.md).
* **Creation**: Added standalone Cloudflare-deployable project landing page and documentation site in `site/` with live dashboard screenshot showcase, feature comparison matrix, and static OKF wiki generator.
* **Update**: Added Server-Sent Events (SSE) streaming mode for real-time dashboard telemetry, broadcasting workflow updates and heartbeats with EventSource token query authorization.
* **Update**: Converted Python sample application to PEP 723 inline script metadata in `examples/python/main.py`, removing `requirements.txt` and enabling direct invocation with `uv run main.py`.
* **Update**: Enhanced dashboard workflow visualization with multi-level workflow family grouping, cubic bezier execution curves between parent steps and child workflows, and interactive canvas viewport with pan, zoom, and dot-grid background.
* **Update**: Updated [Dashboard manual acceptance checklist](manual/dashboard-acceptance.md) to cover canvas viewport controls, workflow family hierarchies, live telemetry polling options, and shortcut cheat sheets.
* **Update**: Overhauled dashboard design tokens to an industrial zinc and technical cobalt palette, achieving WCAG AA/AAA contrast ratios, tabular lining numerals, authored inline SVGs, and intentional alternatives under `prefers-reduced-motion`.
* **Creation**: Designed bespoke brandmark and favicon ("Circuit Handoff R") representing durable state control, orchestration consensus, and dynamic executor dispatch, replacing generic clipart.

## 2026-09-17

* **Creation**: Added [Dashboard manual acceptance checklist](manual/dashboard-acceptance.md) documenting systematic verification across fleet management, workflows, step graph visualizations, schedules, queues, and alerting rules.
* **Update**: Expanded [Conductor Protocol (WebSocket)](protocol/executor-ws.md) covering all 32 wire protocol message types and verified bidirectional codec round trips across full request and response golden fixture corpus.
* **Update**: Aligned executor heartbeat ping interval (10 seconds) and pong deadline (25 seconds) timing constants with upstream SDK client contracts.
* **Update**: Enforced fine-grained authorization role checks and deny-by-default access policies for empty API key permissions.
* **Update**: Added atomic check-and-set rule throttling for multi-instance alert dispatch and lease fencing for high-availability executor adoption.
* **Update**: Added workflow retention scheduling and background sweep lifecycle management.
* **Update**: Added multi-language SDK verification matrix workflow and dual-instance scaling deployment stack with reverse proxy routing.
* **Update**: Clarified application autoscaling policy as an orchestrator responsibility and deferred non-goal in [Goals and boundaries](design/goals.md).
* **Update**: Clarified workflow recovery lifecycle test verification via chaos testing harness (`tests/chaos/chaos_test.go`) in [Conformance testing](usage/conformance.md).
* **Update**: Documented multi-SDK system database schema compatibility requirements for direct data-plane queries in [Data plane](architecture/dataplane.md).

## 2026-09-14

* **Creation**: Added [Production Hardening](usage/hardening.md) documenting in-process TLS certificate configuration and reverse proxy access log restrictions against recording WebSocket URIs.
* **Creation**: Added [ADR 0010](adr/0010-openapi-redistribution.md) recording the provenance, attribution posture, and clean-room redistribution rationale for vendored OpenAPI interface specifications.
* **Update**: Added database migration management subcommands (status, force, down, up) and skip-migrations support in [Operations and migrations](usage/operations.md).
* **Update**: Reconciled autoscale REST operations in [REST surface discovery](discovery/rest-surface.md) as uninstalled policy 404 Problem Details responses and deferred Tier 5 capabilities.
* **Update**: Added startup lease reconciliation, periodic orphan adoption, and lease heartbeats to High Availability coordinator.
* **Update**: Added recovery dispatch attempt recording for successes and failures, and verified pending workflow counts before deleting dead records on cross-version recovery.
* **Update**: Added grace-period timer cancellation to eliminate goroutine leaks across executor disconnect and reconnect cycles.
* **Update**: Aligned alerting rule evaluation for WorkflowFailure and SlowQueue with Conductor WebSocket queries (list_workflows and list_queued_workflows) over registered executors.
* **Update**: Added SSRF dialer protection (blocking private, link-local, loopback, multicast, and non-HTTP/HTTPS destinations) and redirect re-validation for webhook, Slack, and PagerDuty alert channel dispatchers.
* **Update**: Added OpenMetrics content negotiation and EOF termination to /v1/metrics while clarifying supported Scale-tier metric families in [Metrics and alerting surface](discovery/metrics-alerting.md).
* **Update**: Reconciled [ADR 0004](adr/0004-data-plane-via-sdk-client.md) to delineate implemented connection pool sizing and per-query statement timeouts from planned scale-tier guardrails.
* **Update**: Updated [Conductor Protocol (WebSocket)](protocol/executor-ws.md) section structure to explicitly document heartbeat, liveness, and timeout semantics.
* **Update**: Added release checklist to [Maintainer guide](contribution/maintainers.md) specifying clean-room legal provenance verification and pre-release history hygiene requirements.

## 2026-09-13

* **Update**: Reconciled executor lifecycle status definitions (`HEALTHY`, `DISCONNECTED`, `DEAD`, `Deleted`), heartbeat timing parameters (20s ping interval, 25s ping wait, 15s/30s pong timeout), and peer failover mechanics in [Recovery](architecture/recovery.md) and [Recovery timing parameters](discovery/recovery-params.md) to match the normative WebSocket protocol specification.
* **Update**: Aligned data-plane documentation in [Data plane](architecture/dataplane.md) with ADR 0004 and ADR 0008, detailing `SkipMigrations: true` schema safety, `VerifyMigrations` checks, deferred serialization boundaries, fork mutation semantics, and separating Conductor alerting rules from Prometheus operational alerts.
* **Update**: Aligned WebSocket alert frame documentation in [Metrics, alerting, and HA peer forwarding](discovery/metrics-alerting.md) with the unidirectional notification contract in [Conductor Protocol (WebSocket)](protocol/executor-ws.md), removing invalid response envelopes and detailing HMAC-SHA256 authenticated peer-forwarding route `/internal/v1/forward/{appID}` with loop prevention headers.
* **Update**: Aligned conformance battery 4 documentation in [Conformance testing](usage/conformance.md) and [Conformance Batteries Specification](discovery/Batteries.md) to specify `fork_workflow` mutations rather than unimplemented restart types, and removed internal discovery shorthand.

## 2026-09-12

* **Update**: Sanitized internal process vocabulary, phase numbering, and legacy document prefix identifiers across all documentation and discovery references, renaming discovery documents to topic-only paths (`rest-surface.md`, `authz.md`, `client-behaviour.md`, `dashboard-reuse.md`, `recovery-params.md`, `metrics-alerting.md`).
* **Update**: Aligned project status, architecture components, and recovery documentation with shipped Go implementation, removing pre-implementation disclaimers and defining executor liveness parameters against [Conductor WebSocket protocol](protocol/executor-ws.md).
* **Update**: Reconciled overview and goals documentation with ADR 0004 data-plane architecture, noting read-by-default SDK client database fallback access and removing outdated database boundary claims.
* **Creation**: Added [ADR 0009](adr/0009-dashboard-web-stack.md) recording the zero-dependency vanilla JS dashboard architecture, bespoke SVG step graph renderer, and manual OpenAPI model maintenance.
* **Update**: Corrected quickstart guide environment variable usage (`DBOS__CONDUCTOR_URL`, `DBOS_URL`, `DBOS_TOKEN`), clarified sample app `RELAY_URL` wiring, and aligned Go prerequisite floor with `go.mod`.
* **Creation**: Added [Operations and migrations](usage/operations.md) documenting startup auto-migration, rolling upgrade expand/contract schema evolution, and database restore rollback procedures.
* **Update**: Documented declarative configuration secret indirection via environment variable expansion, runtime timing overrides, `--env-out` flag, and repository gitignore refusal checks.
* **Update**: Regenerated client command endpoints and acceptance checklists in [Client behaviour](discovery/client-behaviour.md) to match upstream `dbos-ctl` client and OpenAPI specifications.
* **Creation**: Added `THIRD-PARTY-LICENSES.md` providing comprehensive attribution and license notices for MIT, BSD-3-Clause, and Apache-2.0 dependencies, including the DBOS Go SDK.
* **Update**: Enhanced `.agents/scripts/check-okf.py` with automated validation for internal ticket prefixes, scratch paths, and minified JSON line citations.
* **Update**: Corrected provenance ledger description in [Clean-room rules](contribution/clean-room.md) as a permitted source register, and replaced minified JSON line citations with standard JSON pointer paths in discovery documentation.
* **Update**: Aligned multi-SDK verification matrix documentation in [Multi-SDK verification matrix](testing/verify-sdk.md) with genuine REST endpoint probes rather than unexercised client CLI binaries, and specified verbatim payload byte preservation rather than unmodelled serialization tags.
* **Update**: Enforced WebSocket read limits, handshake timeouts, and error message rejections in connection hub.
* **Update**: Implemented heartbeat pong deadlines, write mutex decoupling, and executor lease ownership renewal.
* **Update**: Hardened connection lifecycle with idempotent closure, identity-aware unregistration, and registry drain on graceful shutdown.
* **Update**: Balanced executor dispatch selection across replicas and added automatic request retry on executor write failure.
* **Update**: Documented synthetic executor disclosure in conformance test reporting and CLI usage guide.
* **Update**: Wired timeout configuration into conformance HTTP client and per-battery execution context.
* **Update**: Hardened conformance test suite scoring logic and added workflow mutation identifier assertions.
* **Update**: Removed duplicate clean-room source listings from agent governance references in favour of the canonical contribution guide.
* **Update**: Updated [ADR 0009](adr/0009-dashboard-web-stack.md) and dashboard component reuse documentation to accurately describe esbuild asset bundling and vanilla JavaScript browser runtime architecture.
* **Update**: Included NOTICE and THIRD-PARTY-LICENSES in release packaging configuration and container distribution images.
* **Update**: Reconciled quickstart guide to use default local organization resolution across key creation and REST query examples.
* **Update**: Documented executor liveness timeout and ping interval parameters directly in recovery architecture guide.
* **Update**: Added exact permitted source citations for workflow status cancellation and resume SQL update semantics.
* **Update**: Clarified data-plane fallback metrics and alert rules as planned observability tier features.
* **Update**: Removed local scratch path references from contributor documentation and project rules.
* **Update**: Strengthened automated OKF documentation bundle validation for minified specification line citation detection.

* **Update**: Rebuilt [Conductor WebSocket protocol specification](protocol/executor-ws.md) covering all 32 implemented message types, normalized envelope definitions, aligned handshake and alert payloads, and added inline citations for idempotence guarantees.
* **Update**: Resolved decoding direction heuristics in protocol codec and added exhaustive round-trip tests for zero-value response structures.
* **Update**: Aligned `MetricData` shape with upstream schema and added golden fixture testing with unknown field rejections.
* **Update**: Added `NOTICE` acknowledging clean-room derivation of wire types from MIT-licensed SDK source and amended [ADR 0001](adr/0001-clean-room-derivation.md).
* **Update**: Replaced loose map interfaces in workflow and step aggregate responses with typed rows mirroring system database schemas across protocol, data-plane, and REST mapping layers.
* **Update**: Corrected schedule property mappings and list workflow filter names in [Wire to REST mapping](protocol/wire-to-rest.md) and removed fabricated HTTP 502 status claims.
* **Update**: Implemented full filter parity, cancellation options, and full status field mapping in data-plane client.
* **Update**: Propagated caller context, statement timeouts, and connection pool bounds to data-plane client instances.
* **Update**: Added transactional mutation support and reconciliation in declarative engine.
* **Update**: Isolated per-application initialisation with dedicated mutexes and failure backoff in data-plane manager.
* **Update**: Added fleet workflow and step aggregate dispatch in data-plane manager and reconciled declarative alert rule types with OpenAPI specification enum in [ADR 0008](adr/0008-alerting-rule-extensions.md).
* **Update**: Replaced regex dashboard bundling with an esbuild pipeline and added bundle parse validation.
* **Update**: Hardened dashboard against HTML injection in inline event handlers and configured Content Security Policy headers.
* **Update**: Aligned dashboard API key management model with OpenAPI specification schemas.
* **Update**: Implemented workflow fork functionality and mapped restart actions to step-zero forks.
* **Update**: Added in-browser authentication modal intercepting unauthenticated API responses under auth mode.
* **Update**: Unified authorization middleware, enforced tenant path and application scoping across REST endpoints, and implemented cross-tenant isolation testing.
* **Update**: Hardened peer-forward endpoint with random cluster secret fallback, replay protection windows, and pre-allocation signature verification.
* **Update**: Enforced role-based access control on identity mutation handlers, organisation secret validation on join, and grantable permission catalogues.
* **Update**: Enforced OIDC audience validation, negative caching for unknown keys, and clock-skew leeway.
* **Update**: Authenticated metrics scrape endpoints under auth mode and sanitized application labels against multi-tenant leakage.
* **Update**: Implemented audit log capture middleware recording client IP, request status, and query filters.
* **Update**: Added webhook SSRF destination validation, secret redaction on alerting rule listings, database error sanitization on health endpoints, and API key usage tracking.
* **Update**: Added per-package test database helper in `internal/testdb` and serialized test execution in Makefile to isolate concurrent database access.
* **Update**: Configured PostgreSQL service user in test-suite workflow to match test connection credentials.
* **Update**: Added `RELAY_VERIFY_SDK=1` environment gate and full cluster container checks to multi-SDK verification matrix.
* **Update**: Added dashboard JavaScript bundle syntax verification step to code quality pipeline.
* **Update**: Documented external alerting notification channels (Webhook with timestamped HMAC-SHA256 signatures, Slack, PagerDuty) under Relay control plane extensions in [Metrics, alerting, and HA peer forwarding](discovery/metrics-alerting.md).
* **Update**: Updated Java SDK verification status in [Multi-SDK verification matrix](testing/verify-sdk.md) to required for socket, presence, chaos, and live fork cells (Cells 1, 2, 3, 5, and 7) backed by dual-executor container orchestration and real timer failover, while documenting upstream schema version divergence (v19 vs v107) for data-plane cells (Cells 4 and 6).

## 2026-09-08

* **Creation**: Added multi-SDK verification test suite in `tests/verifysdk` and real Podman compose cluster in `deploy/compose-sdk-apps.yaml` running genuine Go, Python, TypeScript, and Java runtimes under Podman, proving socket presence, conformance probes, data-plane reading, parity, real timer chaos recovery, offline cancel/resume, and live fork dequeuing.
* **Correction**: Corrected resume SQL status predicate to `status NOT IN ('SUCCESS', 'ERROR')` in [Data plane](architecture/dataplane.md), aligned with Go, Python, and TypeScript implementations, and documented durable sleep and message receive cancellation semantics across SDKs.
* **Correction**: Corrected Problem Details specification citation to RFC 9457 in [Client behaviour](discovery/client-behaviour.md).
* **Update**: Documented wire and data-plane parity for cancellation and resume operations, step-boundary outcome check semantics across SDKs, and served-from auditing in [Data plane](architecture/dataplane.md).
* **Creation**: Added [Declarative operations](usage/declarative.md) documenting
  `relay.yaml` manifest format, `relay diff`, and `relay apply`.
* **Creation**: Added [Data plane](architecture/dataplane.md) documenting
  optional application database access through the Go SDK client, fallback
  routing, and safety invariants.
* **Update**: Updated database access invariant in `AGENTS.md` to permit optional
  data-plane reads and mutations through the official SDK client library.
* **Creation**: Added [ADR 0004](adr/0004-data-plane-via-sdk-client.md) recording
  optional data-plane access via the SDK client.
* **Creation**: Added [ADR 0005](adr/0005-rejected-executor-sidecar.md),
  [ADR 0006](adr/0006-rejected-tsnet-connectivity-tunnel.md), and
  [ADR 0007](adr/0007-rejected-native-postgres-extension.md) recording rejected
  explorations for executor sidecars, network tunnels, and native Postgres extensions.
* **Creation**: Implemented the automated DBOS Conductor conformance test suite in `internal/conformance`, providing in-process and blackbox test runners across 8 specification batteries, the `relay test-conformance` CLI command, the `make test/conformance` target, and documentation in [conformance testing](usage/conformance.md).
* **Creation**: Implemented the identity and access control layer featuring RSA OIDC JWT token validation with in-memory JWKS caching and fail-closed key set guarantees, RFC 8628 device flow login compatibility, Postgres persistence for users, roles, organisation members, domain claims, and audit logs, automatic user registration and domain-claim organisation matching, conditional route gating returning 404 Problem Details when running in unauthenticated mode, and an integration test suite in `tests/identity/`.
* **Creation**: Implemented the web dashboard in `console/` and `internal/dashboard`, featuring an OpenAPI-aligned TypeScript data layer, fleet and application overview, workflow search with interactive SVG step execution graph, queue and schedule controls, alerting rule manager, and scoped API key minting.
* **Update**: Documented web dashboard access in the [quickstart guide](usage/quickstart.md).
* **Creation**: Implemented the scale and operational architecture featuring high-availability instance clustering with lease adoption, HMAC-SHA256 signed HTTP peer forwarding with loop prevention, an OpenMetrics scrape endpoint at `/v1/metrics`, alerting rule management REST API, background alert evaluation loop dispatching WebSocket notifications to executors, and a multi-node integration test suite.
* **Update**: Documented alerting WebSocket dispatch and multi-instance peer forwarding in [Executor WebSocket Protocol](protocol/executor-ws.md).
* **Creation**: Implemented the workflow recovery engine featuring an isolated pure lifecycle state machine, deterministic virtual timer clock, version-preferring recovery dispatcher with strict tenant isolation, connection hub integration, and an end-to-end chaos test suite.
* **Update**: Documented executor lifecycle state transitions, timeout parameters, and recovery failover mechanics in [Executor WebSocket Protocol](protocol/executor-ws.md).
* **Creation**: Implemented the REST API surface with typed OpenAPI 3.0 code-generation, router-mediated executor dispatch, application metadata and token persistence in Postgres, and conformance test suite for upstream `dbosctl` compatibility.
* **Update**: Updated the [quickstart guide](usage/quickstart.md) with REST API querying, `dbosctl` CLI usage, and RFC 9457 Problem Details error responses.
* **Update**: Expanded [Wire to REST Mapping](protocol/wire-to-rest.md) with comprehensive field and type mappings for workflows, steps, events, notifications, queues, and schedules.
* **Creation**: Implemented the protocol codec, connection hub, and executor endpoints with sample applications in Python and TypeScript.
* **Refactor**: Consolidated OpenAPI specification access and YAML conversion
  in `api/spec`, eliminated redundant schema decoding across packages, and
  sanitized internal verification identifiers across discovery documents.
* **Creation**: Opened the [usage](usage/index.md) section with
  [Quickstart](usage/quickstart.md) for running Relay locally with Postgres.
* **Update**: Added development targets to the [contributor guide](contribution/guide.md)
  and noted completion of discovery findings and initial foundation packages.
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
  initial discovery was opened to answer. The record also fixes the supporting
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
* Added [REST surface](discovery/rest-surface.md): 64 operations inventoried,
  OIDC-gated routes identified, and OpenAPI specs vendored in `api/spec/`.
* Added [Authorization](discovery/authz.md): permission catalog, role
  model, and API key format.
* Added [Client behaviour](discovery/client-behaviour.md): command reference
  and conformance checklist for `dbosctl`.
* Added [Executor WebSocket protocol](protocol/executor-ws.md) and
  [Wire to REST mapping](protocol/wire-to-rest.md): protocol messages, liveness,
  and recovery guarantees.
* Seeded [provenance ledger](discovery/provenance.md) with confirmed public sources.
* Added [Dashboard reuse](discovery/dashboard-reuse.md): UI component assessment
  and No-Go decision on `@dbos-argus/ui`.
* Added [Recovery timing](discovery/recovery-params.md): heartbeat intervals,
  executor grace periods, and per-app configuration overrides.
* Added [Metrics and alerting](discovery/metrics-alerting.md): OpenMetrics
  catalogue, alerting schemas, and HA peer forwarding conventions.
