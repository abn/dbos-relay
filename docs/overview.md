---
type: Reference
title: Overview
description: What Relay is and why it exists.
tags: [project, introduction]
status: draft
---

# Overview

This page describes what Relay is for and how it is meant to work. None of it
is shipped behaviour: there is no binary yet. Read it as the design, and see
the [compatibility tiers](design/compatibility-tiers.md) for the order in
which parts of it become real.

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

Relay is that control plane, rebuilt in the open. It targets the two contracts
the ecosystem already uses:

- the **executor WebSocket protocol** that the open-source SDKs implement, so
  an application should need no code change to connect, and
- the **Conductor HTTP API**, published as an OpenAPI document, so the
  existing command-line client and generated clients should work against it.

The comparison that explains the shape of the project is headscale, the
open-source control server for Tailscale. The client half is already open
source and well specified; what is missing is the server the clients talk to,
and the protocol between them can be recovered from the clients.

## What Relay will hold

Relay is metadata-only by design. Its own small Postgres database is intended
to hold organisations, applications, executors, instances, API keys, alert
rules, audit entries, and its own metric samples.

It will never connect to an application's database. Executors open outbound
WebSockets to Relay, and every read and every mutation of workflow data is
dispatched over that socket to an executor, which answers from its own system
database. This is a hard boundary rather than a current limitation. See
[components](architecture/components.md).

## What Relay will do

- Accept executor connections, authenticate them, and track liveness.
- Detect a dead executor and ask a healthy one to recover its workflows. See
  [recovery](architecture/recovery.md).
- Serve the HTTP API by routing each request to a healthy executor and mapping
  the answer onto the published schema.
- Expose a dashboard for applications, executors, workflows, queues, and
  schedules.
- Run as a single binary with one required setting, the database URL.

## How it is built

Relay is built clean-room from public sources. The proprietary implementation
is never downloaded, run, observed, or benchmarked. The canonical list of what
may and may not be used is in [the clean-room
rules](contribution/clean-room.md), and the reasoning is in [ADR
0001](adr/0001-clean-room-derivation.md).

## Status

Discovery. The current work is the executor protocol specification, the
vendored REST contract, and the stack decision. See [goals and
non-goals](design/goals.md) for the v1 boundary.

Facts about the ecosystem on this page come from the public documentation
listed under [sources](contribution/clean-room.md#sources). They are
re-verified during discovery and recorded in the provenance ledger.
