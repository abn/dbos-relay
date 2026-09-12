---
type: Decision
title: ADR 0008 - Alerting Rule Extensions
description: Extend built-in metrics and alerting with RecoveryFlapping and StrandedVersion rule types.
status: accepted
---

# ADR 0008: Alerting Rule Extensions

## Status
Accepted

## Context
The system provides built-in metrics and alerting for `WorkflowFailure`, `SlowQueue`, and `UnresponsiveApplication`. We are expanding this to support two new scenarios critical for long-running workflows and multi-version deployments:

1. **RecoveryFlapping**: Workflows that repeatedly fail and recover within a short time window.
2. **StrandedVersion**: Workflows that remain active on an old application version long after a new version is deployed.

## Decision
We extend the alert rule definitions and delivery payloads to include `RecoveryFlapping` and `StrandedVersion` rule types.
These types are added to the REST read/write schema and the declarative configuration parser.

## Consequences
- The declarative YAML configuration parser has been updated to accept the new types.
- The `D8-metrics-alerting.md` documentation has been updated to reflect the new `enum` values and metadata structures.
- Clients relying on strict enum checking for alerting rules will need to accept the two new types.
