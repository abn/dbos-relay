---
okf_version: "0.2"
---

# Relay documentation

Relay is an open-source control plane for DBOS Transact applications,
compatible with the Conductor executor protocol and HTTP API. It is not
affiliated with or endorsed by DBOS, Inc.

This is an Open Knowledge Format v0.2 bundle: a human- and agent-readable wiki
covering the project's public design, architecture, decisions, and
contribution guidance. It is maintained to reflect status quo as the project
evolves. The working specification is held outside this bundle.

The project implementation is in progress. Sections describe the architecture, protocol,
and operational usage of Relay.

## Getting started

* [Overview](overview.md) - what Relay is and why it exists
* [Contributor guide](contribution/guide.md) - how to contribute
* [Clean-room rules](contribution/clean-room.md) - the sourcing rules that
  bind every contributor

## Usage

* [Usage overview](usage/index.md) - how to run and operate Relay
* [Quickstart](usage/quickstart.md) - run Relay locally with Postgres
* [Declarative operations](usage/declarative.md) - fleet configuration with relay.yaml, apply, and diff
* [Operations and migrations](usage/operations.md) - schema migrations, rolling upgrades, and rollbacks
* [Conformance testing](usage/conformance.md) - run the automated Conductor conformance test suite

## Testing

* [Testing overview](testing/index.md) - verification matrices and test suites
* [Multi-SDK verification](testing/verify-sdk.md) - test matrix across Python, TypeScript, Go, and Java SDKs

## Discovery

* [Discovery overview](discovery/index.md) - initial discovery findings
* [Provenance ledger](discovery/provenance.md) - permitted sources for every
  claim
* [REST surface](discovery/rest-surface.md) - HTTP API operations and inventory
* [Authorization](discovery/authz.md) - permission model and key format
* [Client behaviour](discovery/client-behaviour.md) - dbosctl conformance notes
* [Dashboard reuse](discovery/dashboard-reuse.md) - UI assessment and go/no-go decision
* [Recovery timing](discovery/recovery-params.md) - heartbeat, grace periods, and overrides
* [Metrics and alerting](discovery/metrics-alerting.md) - OpenMetrics catalogue and alert rules

## Protocol

* [Protocol overview](protocol/index.md) - executor WebSocket protocol
  specification
* [Executor WebSocket protocol](protocol/executor-ws.md) - wire messages and liveness
* [Wire to REST mapping](protocol/wire-to-rest.md) - mapping wire records to OpenAPI schemas

## Design

* [Design overview](design/index.md) - the high-level approach
* [Goals and non-goals](design/goals.md) - what v1 does and does not cover
* [Compatibility tiers](design/compatibility-tiers.md) - the milestone gates
* [Terminology](design/terminology.md) - the canonical vocabulary

## Architecture

* [Architecture overview](architecture/index.md) - components and layout
* [Components](architecture/components.md) - the module boundaries
* [Recovery](architecture/recovery.md) - executor liveness and recovery

## Decisions

* [Architecture decision records](adr/index.md) - decision records

## Contribution

* [Contribution overview](contribution/index.md) - guides for contributors
  and maintainers

## Repository contract

`AGENTS.md` holds the operational contract for humans and agents. This wiki
never contains internal names, codenames, hostnames, absolute paths, tokens,
or task identifiers, and the project's internal progress log is not part of
it.

## Knowledge base

* [Changelog](changelog.md) - software release notes managed by Release Please
* [Documentation log](log.md) - how this wiki has evolved
