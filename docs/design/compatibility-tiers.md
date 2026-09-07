---
type: Reference
title: Compatibility tiers
description: The compatibility gates that order the work and define done.
status: draft
---

# Compatibility tiers

Work is ordered by compatibility rather than by feature. Each tier is a gate,
and a gate opens on a demonstration with captured output.

| Tier | Scope | Gate |
| --- | --- | --- |
| Connect | Executor handshake, authentication, heartbeat, executor listing | Sample applications in at least two SDK languages appear in the executor listing |
| Observe | Read operations across workflows, queues, schedules, steps, events, notifications, streams, and aggregates | The conformance suite and the upstream client's read commands pass |
| Operate | Cancel, resume, fork, delete and their bulk forms; schedule control; export and import; versions | Conformance and the upstream client's mutation commands pass |
| Recover | Disconnected and dead state machine, recovery dispatch, confirmation, executor cleanup | Chaos test: kill one executor of several, every workflow completes elsewhere, no outcome runs twice |
| Scale | Ownership and peer forwarding, metrics, alert rules, retention, audit log | Two instances behind a proxy route correctly and a metrics scrape works |
| Dashboard | The web interface | Manual acceptance checklist |
| Identity | OIDC, users, organisations, roles, permissions, and the operations that require them | The upstream client's device login works against Relay and a test identity provider in CI |

The tiers are also the milestone plan. A tier is never partially claimed. If
the acceptance condition has not been demonstrated, the tier is open, however
much of its code exists.
