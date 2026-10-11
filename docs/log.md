# Documentation update log

This log records changes to the documentation bundle: page additions,
renames and deprecations, structural refactors, and corrections. Feature
and code records live in the software [Changelog](changelog.md), which
Release Please maintains from the commit history.

## Unreleased

* **Update**: Added the [Supported SDKs](usage/supported-sdks.md) page recording the supported executor SDK range as policy, with each version claim cited against its SDK repository tag and its package-registry upload date, and added a section to [Multi-SDK verification](testing/verify-sdk.md) naming the offline ceiling check. Linked both from the usage and documentation indexes.
* **Update**: Closed the review follow-ups from the `is_fork` fold. [Data plane](architecture/dataplane.md) records that the local SDK-client fallback does not apply the filter and returns a superset, with the upstream commits and releases that added it; the wire catalogue in [Executor WebSocket protocol](protocol/executor-ws.md) lists `is_fork` on `list_workflows`; the codec suite pins that a nil filter leaves the key off the wire; the data-plane field-coverage guard now fails when a field listed as knowingly unmapped starts being applied by the data plane; and the two handler provenance comments carry the current specification fetch date.
* **Update**: Recorded the `is_fork` workflow filter across the [wire catalogue](protocol/executor-ws.md), the [wire to REST mapping](protocol/wire-to-rest.md) and the [Conformance Batteries](discovery/Batteries.md), and re-vendored the Conductor v2 OpenAPI documents with the matching `isFork` property (fetched 2026-10-11).
* **Creation**: Added [ADR 0013](adr/0013-out-of-line-workflow-payloads.md) recording the decision to keep the Go SDK data-plane client pinned at v1.3.0 and treat reads of out-of-line workflow payloads as a documented capability gap. Corrected the multi-SDK schema claim in [Data plane](architecture/dataplane.md), added a qualifying addendum to [ADR 0004](adr/0004-data-plane-via-sdk-client.md), and registered the tagged Go SDK sources in the [provenance ledger](discovery/provenance.md).
* **Update**: Aligned user-facing docs with the merged rewind and token-rename changes: the protocol message count is now 33 in the README and landing page, `dbosctl api-key rename` is documented in [Client behaviour](discovery/client-behaviour.md), the Operate and Identity tiers list rewind and API key rename, and [Conformance Batteries](discovery/Batteries.md) adds the Battery 4 rewind check. Corrected the `relay test-conformance` help text and the conformance-drift skill to name Fork rather than Restart.
* **Update**: Refreshed the provenance ledger and citations to the current upstream commits (Go `fb3e33e`, Python `2b93e14`, TypeScript `749a4d4`, Java `ecc2bda`, `dbos-ctl` `1e6d20e`, Argus `66ae6da`), renamed the Go SDK repository reference to `dbos-transact-golang`, corrected the `restart` note (Python and TypeScript removed it), and recorded that `retention` is acknowledged asynchronously.
* **Update**: Recorded the queue partition fields in the [wire to REST mapping](protocol/wire-to-rest.md) as they pass from `list_queues` and `get_queue` executor frames to the `Queue` schema.
* **Update**: Re-vendored the Conductor v2 OpenAPI documents (fetched 2026-09-30) and added the `rewindWorkflow` and `updateToken` operations to the [REST surface](discovery/rest-surface.md) inventory, recording the dropped partition restriction on autoscaling policy queues and the raised application name length as open divergences.
* **Update**: Documented shared-database retention semantics, the resume executor-health requirement and the `restart` divergence in [Workflow retention](usage/retention.md).
* **Update**: Documented OIDC profile setup in [Quickstart](usage/quickstart.md).
* **Creation**: Added [ADR 0012](adr/0012-autoscaling-policy-support.md) and the [Autoscaling](usage/autoscaling.md) page covering queue-driven executor recommendations for external scalers, with Relay recommending counts and actuation left to the orchestrator.
* **Update**: Extended the [dashboard acceptance checklist](manual/dashboard-acceptance.md) to cover the Settings, Autoscaling and Audit Log views.

## 2026-09-18

* **Creation**: Added the software [Changelog](changelog.md), rendered on the
  documentation site and reserved in the OKF bundle validator.
* **Creation**: Added [ADR 0011](adr/0011-dual-engine-storage-architecture.md)
  establishing dual-engine storage architecture with PostgreSQL as flagship
  production engine, pure-Go SQLite for zero-dependency embedded mode, Turso
  MVCC for scale-out, and future adoption of Turso's PostgreSQL wire mode.
* **Update**: Converted the ASCII architecture and lifecycle diagrams across
  the docs to Mermaid flowcharts and sequence diagrams with a vendored offline
  runtime, added Chroma syntax highlighting to wiki rendering, made the
  documentation sidebar sections collapsible, and unified section headings
  with section index routes.
* **Update**: Documented the multi-database fleet architecture and the design
  rationale for omitting database credentials and topology management from the
  control plane UI in
  [Data plane access via SDK client](architecture/dataplane.md).
* **Creation**: Added the standalone Cloudflare-deployable landing page and
  documentation site in `site/`, with a live dashboard screenshot showcase,
  feature comparison matrix, and static OKF wiki generator.
* **Update**: Updated
  [Dashboard manual acceptance checklist](manual/dashboard-acceptance.md) to
  cover canvas viewport controls, workflow family hierarchies, live telemetry
  polling options, and shortcut cheat sheets.
* **Update**: Overhauled the dashboard design tokens to an industrial zinc and
  technical cobalt palette, achieving WCAG AA and AAA contrast ratios, tabular
  lining numerals, authored inline SVGs, and intentional alternatives under
  `prefers-reduced-motion`.
* **Creation**: Designed the bespoke brandmark and favicon ("Circuit Handoff R")
  representing durable state control, orchestration consensus, and dynamic
  executor dispatch, replacing generic clipart.

## 2026-09-17

* **Creation**: Added
  [Dashboard manual acceptance checklist](manual/dashboard-acceptance.md)
  documenting systematic verification across fleet management, workflows, step
  graph visualisations, schedules, queues, and alerting rules.
* **Update**: Expanded [Conductor Protocol (WebSocket)](protocol/executor-ws.md)
  to cover all 32 wire protocol message types.
* **Update**: Clarified that application autoscaling policy is an orchestrator
  responsibility and a deferred non-goal in
  [Goals and boundaries](design/goals.md).
* **Update**: Recorded in [Conformance testing](usage/conformance.md) that the
  workflow recovery lifecycle is verified through the chaos testing harness.
* **Update**: Documented the multi-SDK system database schema compatibility
  requirements for direct data-plane queries in
  [Data plane](architecture/dataplane.md).

## 2026-09-14

* **Creation**: Added [Production Hardening](usage/hardening.md) documenting
  in-process TLS certificate configuration and reverse proxy access log
  restrictions against recording WebSocket URIs.
* **Creation**: Added [ADR 0010](adr/0010-openapi-redistribution.md) recording
  the provenance, attribution posture, and clean-room redistribution rationale
  for vendored OpenAPI interface specifications.
* **Update**: Documented the database migration management subcommands and
  skip-migrations support in
  [Operations and migrations](usage/operations.md).
* **Update**: Reconciled the autoscale REST operations in
  [REST surface discovery](discovery/rest-surface.md) as uninstalled policy
  404 Problem Details responses and deferred Tier 5 capabilities.
* **Update**: Clarified the supported Scale-tier metric families in
  [Metrics, alerting, and HA peer forwarding](discovery/metrics-alerting.md).
* **Update**: Reconciled [ADR 0004](adr/0004-data-plane-via-sdk-client.md) to
  delineate implemented connection pool sizing and per-query statement timeouts
  from planned scale-tier guardrails.
* **Update**: Updated the [Conductor Protocol (WebSocket)](protocol/executor-ws.md)
  section structure to document heartbeat, liveness, and timeout semantics.
* **Update**: Added a release checklist to
  [Maintainer guide](contribution/maintainers.md) specifying clean-room legal
  provenance verification and pre-release history hygiene requirements.

## 2026-09-13

* **Update**: Reconciled the executor lifecycle status definitions (`HEALTHY`,
  `DISCONNECTED`, `DEAD`, `Deleted`), heartbeat timing parameters, and peer
  failover mechanics in [Recovery](architecture/recovery.md) and
  [Recovery timing parameters](discovery/recovery-params.md) against the
  normative WebSocket protocol specification.
* **Update**: Aligned the data-plane documentation in
  [Data plane](architecture/dataplane.md) with ADR 0004 and ADR 0008, detailing
  `SkipMigrations: true` schema safety, `VerifyMigrations` checks, deferred
  serialization boundaries, fork mutation semantics, and the separation of
  Conductor alerting rules from Prometheus operational alerts.
* **Update**: Aligned the WebSocket alert frame documentation in
  [Metrics, alerting, and HA peer forwarding](discovery/metrics-alerting.md)
  with the unidirectional notification contract in
  [Conductor Protocol (WebSocket)](protocol/executor-ws.md), removing the
  invalid response envelopes and detailing the HMAC-SHA256 authenticated
  peer-forwarding route `/internal/v1/forward/{appID}` with loop prevention
  headers.
* **Update**: Aligned the conformance battery 4 documentation in
  [Conformance testing](usage/conformance.md) and
  [Conformance Batteries Specification](discovery/Batteries.md) to specify
  `fork_workflow` mutations rather than unimplemented restart types, and
  removed internal discovery shorthand.

## 2026-09-12

* **Update**: Sanitized internal process vocabulary, phase numbering, and
  legacy document prefix identifiers across all documentation and discovery
  references, renaming the discovery documents to topic-only paths
  (`rest-surface.md`, `authz.md`, `client-behaviour.md`, `dashboard-reuse.md`,
  `recovery-params.md`, `metrics-alerting.md`).
* **Update**: Aligned the project status, architecture components, and recovery
  documentation with the shipped Go implementation, removing
  pre-implementation disclaimers and defining executor liveness parameters
  against [Conductor Protocol (WebSocket)](protocol/executor-ws.md).
* **Update**: Reconciled the overview and goals documentation with the ADR 0004
  data-plane architecture, noting read-by-default SDK client database fallback
  access and removing outdated database boundary claims.
* **Creation**: Added [ADR 0009](adr/0009-dashboard-web-stack.md) recording the
  zero-dependency vanilla JS dashboard architecture, bespoke SVG step graph
  renderer, and manual OpenAPI model maintenance.
* **Correction**: Corrected the quickstart environment variable usage
  (`DBOS__CONDUCTOR_URL`, `DBOS_URL`, `DBOS_TOKEN`), clarified the sample app
  `RELAY_URL` wiring, and aligned the Go prerequisite floor with `go.mod`.
* **Creation**: Added [Operations and migrations](usage/operations.md)
  documenting startup auto-migration, rolling upgrade expand/contract schema
  evolution, and database restore rollback procedures.
* **Update**: Documented declarative configuration secret indirection via
  environment variable expansion, runtime timing overrides, the `--env-out`
  flag, and the repository gitignore refusal checks in
  [Declarative operations](usage/declarative.md).
* **Update**: Regenerated the client command endpoints and acceptance
  checklists in [Client behaviour](discovery/client-behaviour.md) to match the
  upstream `dbosctl` client and OpenAPI specifications.
* **Creation**: Added `THIRD-PARTY-LICENSES.md` with attribution and licence
  notices for the MIT, BSD-3-Clause, and Apache-2.0 dependencies, including the
  DBOS Go SDK.
* **Update**: Documented the synthetic executor disclosure in the conformance
  test reporting and the CLI usage guide in
  [Conformance testing](usage/conformance.md).
* **Correction**: Corrected the provenance ledger description in
  [Clean-room rules](contribution/clean-room.md) as a permitted source
  register, and replaced minified JSON line citations with standard JSON
  pointer paths in the discovery documentation.
* **Update**: Aligned the multi-SDK verification matrix documentation in
  [Multi-SDK verification matrix](testing/verify-sdk.md) with genuine REST
  endpoint probes rather than unexercised client CLI binaries, and specified
  verbatim payload byte preservation rather than unmodelled serialization
  tags.
* **Update**: Removed the duplicated clean-room source listings from the agent
  governance references, leaving the
  [contribution guide](contribution/guide.md) the canonical copy.
* **Update**: Updated [ADR 0009](adr/0009-dashboard-web-stack.md) and
  [Dashboard component reuse evaluation](discovery/dashboard-reuse.md) to
  describe the esbuild asset bundling and vanilla JavaScript browser runtime
  architecture.
* **Correction**: Reconciled the quickstart guide to use default local
  organization resolution across key creation and REST query examples.
* **Update**: Documented the executor liveness timeout and ping interval
  parameters in [Recovery](architecture/recovery.md).
* **Update**: Added exact permitted source citations for the workflow status
  cancellation and resume SQL update semantics in
  [Data plane](architecture/dataplane.md).
* **Update**: Clarified the data-plane fallback metrics and alert rules as
  planned observability tier features in
  [Data plane](architecture/dataplane.md).
* **Update**: Removed local scratch path references from the contributor
  documentation and project rules.
* **Update**: Rebuilt [Conductor Protocol (WebSocket)](protocol/executor-ws.md)
  covering all 32 implemented message types with normalized envelope
  definitions, aligned handshake and alert payloads, and inline citations for
  the idempotence guarantees.
* **Creation**: Added `NOTICE` acknowledging the clean-room derivation of wire
  types from MIT-licensed SDK source and amended
  [ADR 0001](adr/0001-clean-room-derivation.md).
* **Correction**: Corrected the schedule property mappings and list workflow
  filter names in [Wire to REST mapping](protocol/wire-to-rest.md) and removed
  the fabricated HTTP 502 status claims.
* **Update**: Reconciled [ADR 0008](adr/0008-alerting-rule-extensions.md) with
  the OpenAPI specification enum for the declarative alert rule types.
* **Update**: Documented the external alerting notification channels (Webhook
  with timestamped HMAC-SHA256 signatures, Slack, PagerDuty) as Relay control
  plane extensions in
  [Metrics, alerting, and HA peer forwarding](discovery/metrics-alerting.md).
* **Update**: Updated the Java SDK verification status in
  [Multi-SDK verification matrix](testing/verify-sdk.md) to required for the
  socket, presence, chaos, and live fork cells (Cells 1, 2, 3, 5, and 7)
  backed by dual-executor container orchestration and real timer failover,
  while documenting the upstream schema version divergence (v19 vs v107) for
  the data-plane cells (Cells 4 and 6).

## 2026-09-08

* **Correction**: Corrected the resume SQL status predicate to
  `status NOT IN ('SUCCESS', 'ERROR')` in
  [Data plane](architecture/dataplane.md), aligned with the Go, Python, and
  TypeScript implementations, and documented the durable sleep and message
  receive cancellation semantics across SDKs.
* **Correction**: Corrected the Problem Details specification citation to
  RFC 9457 in [Client behaviour](discovery/client-behaviour.md).
* **Update**: Documented the wire and data-plane parity for cancellation and
  resume operations, step-boundary outcome check semantics across SDKs, and
  served-from auditing in [Data plane](architecture/dataplane.md).
* **Creation**: Added [Declarative operations](usage/declarative.md)
  documenting the `relay.yaml` manifest format, `relay diff`, and
  `relay apply`.
* **Creation**: Added [Data plane](architecture/dataplane.md) documenting
  optional application database access through the Go SDK client, fallback
  routing, and safety invariants.
* **Update**: Updated the database access invariant in `AGENTS.md` to permit
  optional data-plane reads and mutations through the official SDK client
  library.
* **Creation**: Added [ADR 0004](adr/0004-data-plane-via-sdk-client.md)
  recording optional data-plane access via the SDK client.
* **Creation**: Added
  [ADR 0005](adr/0005-rejected-executor-sidecar.md),
  [ADR 0006](adr/0006-rejected-tsnet-connectivity-tunnel.md), and
  [ADR 0007](adr/0007-rejected-native-postgres-extension.md) recording
  rejected explorations for executor sidecars, network tunnels, and native
  Postgres extensions.
* **Creation**: Added [Conformance testing](usage/conformance.md) documenting
  the conformance test suite, its in-process and blackbox runners, the
  `relay test-conformance` command, and the `make test/conformance` target.
* **Update**: Documented web dashboard access in the
  [quickstart guide](usage/quickstart.md).
* **Update**: Documented alerting WebSocket dispatch and multi-instance peer
  forwarding in
  [Executor WebSocket Protocol](protocol/executor-ws.md).
* **Update**: Documented executor lifecycle state transitions, timeout
  parameters, and recovery failover mechanics in
  [Executor WebSocket Protocol](protocol/executor-ws.md).
* **Update**: Updated the [quickstart guide](usage/quickstart.md) with REST API
  querying, `dbosctl` CLI usage, and RFC 9457 Problem Details error responses.
* **Update**: Expanded [Wire to REST mapping](protocol/wire-to-rest.md) with
  comprehensive field and type mappings for workflows, steps, events,
  notifications, queues, and schedules.
* **Refactor**: Sanitized internal verification identifiers across the
  discovery documents.
* **Creation**: Opened the [usage](usage/index.md) section with
  [Quickstart](usage/quickstart.md) for running Relay locally with Postgres.
* **Update**: Added development targets to the
  [contributor guide](contribution/guide.md) and noted completion of the
  discovery findings and initial foundation packages.
* **Update**: Added governance invariants to the
  [contributor guide](contribution/guide.md) and
  [maintainer guide](contribution/maintainers.md) on scope tightness, avoiding
  duplication and divergence, and keeping committed files free of internal
  process references.
* **Update**: Linked the usage section from the root [index](index.md).
* **Creation**: Opened the [discovery](discovery/index.md) section and its
  [provenance ledger](discovery/provenance.md) recording permitted public
  sources for clean-room derivation.
* **Creation**: Opened the [protocol](protocol/index.md) section index for the
  executor WebSocket specification and wire mappings.
* **Creation**: Added [ADR 0003](adr/0003-implementation-language.md) recording
  Go as the implementation language, which closes the stack question discovery
  was opened to answer. The record also fixes the supporting library choices,
  including the corrected import path for the WebSocket library, whose old
  path appears in older design notes.
* **Update**: Linked both the discovery and protocol sections from the root
  [index](index.md).
* **Update**: [Components](architecture/components.md) now names the planned Go
  package for each module. The page still describes an intended design, and
  says so.

## 2026-09-07

* **Creation**: Opened the bundle with the operational baseline. Added the root
  index, this log, the [overview](overview.md), the design section
  ([goals](design/goals.md), [compatibility
  tiers](design/compatibility-tiers.md), [terminology](design/terminology.md)),
  the architecture section ([components](architecture/components.md),
  [recovery](architecture/recovery.md)), the first two decision records, and
  the contribution section ([guide](contribution/guide.md),
  [clean-room rules](contribution/clean-room.md),
  [maintainer guide](contribution/maintainers.md)).
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
* Added [Authorization](discovery/authz.md): permission catalog, role model,
  and API key format.
* Added [Client behaviour](discovery/client-behaviour.md): command reference
  and conformance checklist for `dbosctl`.
* Added [Executor WebSocket protocol](protocol/executor-ws.md) and
  [Wire to REST mapping](protocol/wire-to-rest.md): protocol messages,
  liveness, and recovery guarantees.
* Seeded [provenance ledger](discovery/provenance.md) with confirmed public
  sources.
* Added [Dashboard reuse](discovery/dashboard-reuse.md): UI component
  assessment and No-Go decision on `@dbos-argus/ui`.
* Added [Recovery timing](discovery/recovery-params.md): heartbeat intervals,
  executor grace periods, and per-app configuration overrides.
* Added [Metrics and alerting](discovery/metrics-alerting.md): OpenMetrics
  catalogue, alerting schemas, and HA peer forwarding conventions.
