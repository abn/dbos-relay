---
type: Decision
title: ADR 0005 - Reject standalone executor-impersonating sidecar
description: Reject running a standalone sidecar that poses as an executor to serve read queries.
status: rejected
---

# ADR 0005 - Reject standalone executor-impersonating sidecar

## Status

Rejected.

## Context

An option considered for providing visibility during application outages was a
standalone sidecar process deployed alongside the application database. The
sidecar would open an outbound WebSocket connection to Conductor or Relay,
advertise itself as a connected executor, and answer workflow read requests
directly from the database.

## Decision

Relay will not build or distribute an executor-impersonating sidecar.

Three considerations make this design unviable:

1. **Recovery misdirection**: Control planes dispatch workflow recovery to
   connected executors. If an impersonating sidecar is connected without
   application workflow code, the control plane may assign dead workflows to it,
   resulting in recovery execution failures or stranded workflows.
2. **Protocol impersonation risk**: Simulating executor protocol semantics to
   feed a proprietary hosted service creates fragile compatibility dependencies
   and ambiguous boundary guarantees.
3. **Redundant for self-hosted Relay**: Relay operators can use the supported
   `internal/dataplane` SDK client connection directly within Relay (ADR 0004).
   If hosted Conductor users need visibility during outages, that is an
   upstream feature request for the hosted service rather than a sidecar hack.

## Consequences

- Eliminates risks of corrupted recovery routing.
- Keeps Relay's operational architecture focused on legitimate executor
  connections and explicit data-plane configurations.
