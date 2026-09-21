---
type: Decision
title: ADR 0012 - Autoscaling policy support
description: Lift the v1 autoscaling non-goal and implement Conductor autoscaling policies and recommendations via executor dispatch.
status: accepted
---

# ADR 0012 - Autoscaling policy support

## Status

Accepted

## Context

The v1 goals (`docs/design/goals.md`) declared workload autoscaling a
non-goal: the container orchestrator owns replica counts, not the metadata
control plane. Relay therefore stubbed the five autoscaling operations
(`getAutoscale`, `getAutoscaleVersion`, `getAutoscalingPolicy`,
`setAutoscalingPolicy`, `deleteAutoscalingPolicy`) with no-policy 404 and
refusal 400 responses.

The Conductor contract for these operations is fully public
(`https://docs.dbos.dev/production/autoscaling`, confirmed 2026-09-21,
and the vendored OpenAPI specification in `api/spec/openapi.json`).
Relay already dispatches every input the formula needs to a healthy
executor: queue definitions (`get_queue`) and queued-workflow listings
(`list_queued_workflows`). The remaining work is a policy row per
application plus arithmetic Relay can do itself, with no application-side
changes and no direct application database queries.

## Decision

Relay implements autoscaling policies and recommendations:

1. **Policy storage**: one autoscaling policy per application (policy
   queue plus optional rollout caps), stored in Relay's own database on
   both engines.
2. **Executor-validated writes**: `PUT` validates the queue against a
   running executor before storing, matching upstream behaviour. The
   queue must exist, must not be partitioned, and must have a worker
   concurrency set.
3. **Executor-computed recommendations**: backlog counts come from
   `list_queued_workflows` dispatch grouped by application version, and
   `desiredExecutors = ceil(queueDepth / workerConcurrency)`, capped by
   the queue concurrency limit and the policy rollout caps. The latest
   version is always reported with at least one executor.
4. **Orchestrator still owns actuation**: Relay recommends counts; it
   never scales anything itself. KEDA `ScaledObject` examples stay in
   documentation.

## Consequences

- The goals non-goal bullet is amended: the control-plane half of
  autoscaling (policies and recommendations) is in scope; actuation
  remains the orchestrator's job.
- Policy writes are audit-logged because the upstream autoscaling guide
  states policy changes appear in the audit log, even though the audit
  taxonomy page publishes no operation strings for them. Relay records
  `autoscaling_policy.set` and `autoscaling_policy.delete` (its own
  names, documented as such) against the application target.
