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

The project is in discovery. Sections describing the implementation appear as
the implementation does.

## Getting started

* [Overview](overview.md) - what Relay is and why it exists
* [Contributor guide](contribution/guide.md) - how to contribute
* [Clean-room rules](contribution/clean-room.md) - the sourcing rules that
  bind every contributor

## Discovery

* [Discovery overview](discovery/index.md) - phase 0 discovery findings
* [Provenance ledger](discovery/provenance.md) - permitted sources for every
  claim

## Protocol

* [Protocol overview](protocol/index.md) - executor WebSocket protocol
  specification

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

* [Documentation log](log.md) - how this wiki has evolved
