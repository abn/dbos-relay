---
type: Reference
title: Overview
description: What Relay is and why it exists.
tags: [project, introduction]
status: draft
---

# Overview

This page describes what Relay is for and how it works. See the
[compatibility tiers](design/compatibility-tiers.md) for supported
capabilities and the [quickstart](usage/quickstart.md) to run it.

## The problem

DBOS Transact is a durable workflow library. An application that uses it keeps
its workflow state in its own Postgres database, and on its own it recovers
workflows when the process that started them comes back. Distributed recovery,
a fleet view, and workflow management come from a separate control plane,
Conductor, which is proprietary and which the vendor's public documentation
describes as licensed for production use.

That leaves a gap. If a machine never comes back, its workflows stay stranded
unless something else notices and hands them to a healthy process.

## What Relay is

Relay is that control plane, rebuilt in the open. It targets the contracts the
ecosystem already uses:

- the **executor WebSocket protocol** that the open-source SDKs implement, for
  Attached mode, so an application on a supported SDK needs no code change to
  connect;
- the **Conductor HTTP API**, published as an OpenAPI document, so the existing
  command-line client and generated clients work against it; and
- the **application system database**, through the official MIT Go client, for
  Direct mode on post-split SDK releases in the supported schema window.

The comparison that explains the shape of the project is headscale, the
open-source control server for Tailscale. The client half was open source and
well specified. The Conductor connector has since moved into a proprietary
package, so the durable integration surface is now the system database and the
MIT external client rather than the connector. See
[ADR 0014](adr/0014-connector-optional-positioning.md) and
[ADR 0015](adr/0015-direct-mode-system-database.md).

## What Relay will hold

Relay is metadata-only by design. Its own small Postgres database is intended
to hold organisations, applications, executors, instances, API keys, alert
rules, audit entries, and its own metric samples.

Relay never executes direct raw SQL against an application's system database.
In Attached mode, executors open outbound WebSockets to Relay, and workflow
reads and mutations are dispatched over that socket to an executor, which
answers from its own system database. In Direct mode, Relay uses the official
MIT Go client against the application's system database when the operator
configures it, so it can serve a fleet with no connected executors. Live
executors take precedence over database reads. Direct mode is planned. See
[ADR 0004](adr/0004-data-plane-via-sdk-client.md),
[ADR 0015](adr/0015-direct-mode-system-database.md), and
[data plane](architecture/dataplane.md).

## What Relay will do

- Accept executor connections, authenticate them, and track liveness.
- Detect a dead executor and ask a healthy one to recover its workflows. See
  [recovery](architecture/recovery.md).
- Serve the HTTP API by routing each request to a healthy executor over
  Attached mode, or by reading the application system database in Direct mode,
  and mapping the answer onto the published schema.
- Observe and control workflows with no connected executor in Direct mode.
- Expose a dashboard for applications, executors, workflows, queues, and
  schedules.
- Run as a single binary with one required setting, the database URL, plus an
  operator-configured application database connection in Direct mode.

## How it is built

Relay is built clean-room from public sources. The proprietary implementation
is never downloaded, run, observed, or benchmarked. The canonical list of what
may and may not be used is in [the clean-room
rules](contribution/clean-room.md), and the reasoning is in [ADR
0001](adr/0001-clean-room-derivation.md).

## Status

Implementation is in progress across the core control plane, executor protocol,
and data plane. See [compatibility tiers](design/compatibility-tiers.md) for milestone
gates and [goals and non-goals](design/goals.md) for the v1 boundary.

Facts about the ecosystem on this page come from the public documentation
listed under [sources](contribution/clean-room.md#sources). They are
re-verified during discovery and recorded in the provenance ledger.
