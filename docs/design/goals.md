---
type: Reference
title: Goals and non-goals
description: The v1 boundary, stated as commitments and refusals.
status: draft
---

# Goals and non-goals

## Goals for v1

- **Executor compatibility in Attached mode.** An unmodified application on a
  supported Attached SDK connects with only the Conductor URL and key changed,
  and appears as a live executor. The supported ceiling is recorded in
  [Supported SDKs](../usage/supported-sdks.md).
- **Direct mode.** On post-split SDKs in the supported schema window, Relay
  observes and controls workflows through the application's own system database
  using the MIT Go client, with no connector and no enterprise package. See
  [ADR 0015](../adr/0015-direct-mode-system-database.md) and
  [Direct mode](../usage/direct.md).
- **Automatic recovery.** In Attached mode, kill an executor and its pending
  workflows are recovered on a healthy executor within the configured grace
  period, with outcomes still exactly-once. In Direct mode, the same outcome
  requires a liveness signal from the deployment.
- **API compatibility.** The upstream command-line client works against a
  Relay URL for every operation it implements, as do clients generated from
  the vendored OpenAPI document.
- **Dashboard.** A web interface that lists applications and executors, lists
  and searches workflows and their steps including the step graph, and can
  cancel, resume, and fork.
- **One binary and a database.** A complete Relay deployment is the binary
  plus its own Postgres URL. Direct mode additionally needs an
  operator-configured connection to each application's system database.
- **Ready for more than one instance.** Executor ownership is recorded in
  Postgres from the first schema, so high availability is added without
  schema churn even though v0 runs a single instance.

## Non-goals for v1

- Cloud platform features such as application deployment or database
  provisioning. They are not part of the API surface being matched.
- Dynamic container scale-out. The control-plane half of autoscaling
  (policies and recommendations, see [ADR 0012](../adr/0012-autoscaling-policy-support.md))
  is in scope; actuating replica counts remains the responsibility of the
  underlying container orchestrator (such as Kubernetes or ECS).
- Multi-organisation tenancy beyond what the specification's organisation
  model requires.
- Day-one parity on every alerting, retention, and audit endpoint. These
  arrive by tier.
- Automated publication of container images (multi-architecture images are planned for a subsequent release).
- Support for pre-1.0 SDK protocol versions.
- Compatibility with the proprietary Conductor connector or the proprietary
  Conductor server. Relay does not inspect or test either.

## Rules that constrain every feature

- Relay never executes direct raw SQL against an application's system database.
  Database access is mediated exclusively through the official SDK client: the
  WebSocket executor in Attached mode, or the MIT Go client in Direct mode.
  Live executors take precedence over database reads. See
  [ADR 0004](../adr/0004-data-plane-via-sdk-client.md) and
  [ADR 0015](../adr/0015-direct-mode-system-database.md).
- Relay never requires an application-side change. If a capability needs the
  SDK to change, Relay keeps working without it in the meantime.
- Where "nicer" and "identical to the documented behaviour" conflict,
  identical wins unless a decision record says otherwise. People will read the
  upstream documentation and expect it to apply.
