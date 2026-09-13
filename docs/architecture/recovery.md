---
type: Reference
title: Recovery
description: The executor liveness state machine and workflow recovery.
status: draft
---

# Recovery

Recovery is the reason a control plane exists. An application on its own
recovers workflows when the process that owned them restarts. With a control
plane, another executor picks them up instead, so a machine that never comes
back does not strand its work.

## The state machine

An executor is connected while its socket is open. When the socket closes it
becomes disconnected. That is a waiting state. A process that reconnects with
the same identifier returns to connected and nothing else happens. If the
grace period elapses without a reconnect, the executor is declared dead.

A dead executor's work is offered to a healthy executor of the same
application, preferring one running the same application version. That
executor is asked to recover the dead executor's workflows. On a successful
reply the dead executor's record is removed and the action is written to the
audit log. On failure or timeout, the next healthy executor is tried with
backoff, and the condition is surfaced as an alert if no executor takes it.

```
connected --socket closed--> disconnected --grace elapsed--> dead
    ^                             |                            |
    \------ reconnect, same id ---/                            |
                                                               v
                                              pick a healthy executor of the
                                              same application, ask it to
                                              recover, confirm, then delete
```

## Rules

- Recovery is idempotent by design. The public documentation describes the
  library's guarantees as at-least-once for steps and exactly-once for
  outcomes, which makes a duplicated recovery request cheap. Prefer sending it
  twice over never sending it. This guarantee is validated across test suites
  and chaos scenarios.
- Never recover across applications or organisations, and never to an executor
  of a different application name.
- If no healthy executor runs the dead executor's version, recovery to the
  latest version happens only when the application's settings allow it.
  Otherwise Relay surfaces the situation and waits for a human.
- Exactly one instance runs the timers for a given executor: the one that owns
  the connection. If that instance dies, another may adopt its orphaned
  executors once the lease expires.
- Every recovery action is written to the audit log.

## How this gets proven

Not by unit tests alone. The gate for this tier is a chaos harness: several
executors of one application against a shared system database, a workload
generator whose workflows leave observable evidence, a killer that terminates
executors on a schedule, and a check that every started workflow reaches a
terminal state exactly once. It runs in CI for any change to liveness or
routing.

Relay implements a default executor timeout of 60 seconds (`executorTimeoutSecs`)
with a 20-second ping heartbeat interval. These defaults may be configured per
application in `relay.yaml` or overridden by application conductor settings.
When an executor connection is interrupted and the timeout duration elapses
without reconnection, the executor is declared dead, and its orphaned workflows
are scheduled for recovery dispatch to a healthy peer.
