---
type: Decision
title: ADR 0008 - Alerting Rule Types Alignment with OpenAPI Specification
description: Reconcile supported alerting rule types with the OpenAPI specification.
status: accepted
---

# ADR 0008: Alerting Rule Types Alignment with OpenAPI Specification

## Status
Accepted

## Context
Upstream DBOS Transact and Conductor define three alerting rule types in their OpenAPI specification:
`WorkflowFailure`, `SlowQueue`, and `UnresponsiveApplication`.

Relay previously considered custom alert rule types (`RecoveryFlapping` and `StrandedVersion`). However, the OpenAPI specification and the REST write endpoints only permit the three standardized rule types. Admitting non-standard rule types through declarative configuration introduces schema drift and causes strictly validating OpenAPI client SDKs to fail when querying alerting rules.

## Decision
Relay reconciles its alerting rule types strictly with the upstream OpenAPI specification enum:
1. `WorkflowFailure`
2. `SlowQueue`
3. `UnresponsiveApplication`

Declarative configuration validation enforces this enum, rejecting unspec'd rule types at configuration time.

## Consequences
- Complete wire and schema parity with upstream Conductor is preserved across REST and declarative workflows.
- Generated API clients decode alerting rule responses without enum validation errors.
- Any future alert rule extensions require upstream protocol additions or namespaced custom endpoints.
