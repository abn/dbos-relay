---
type: Decision
title: ADR 0006 - Reject tsnet executor connectivity tunnel
description: Reject embedding mesh or reverse tunnel overlays for executor communication.
status: rejected
---

# ADR 0006 - Reject tsnet executor connectivity tunnel

## Status

Rejected.

## Context

An architectural exploration considered embedding Tailscale (`tsnet`) or a
similar mesh networking tunnel into Relay and executors to facilitate secure,
cross-network executor connectivity.

## Decision

Relay will not incorporate `tsnet` or reverse tunnel overlays for executor
communication.

The reasons for rejection:

1. **Executors already dial outbound**: In DBOS architecture, executors initiate
   outbound WebSocket connections to Relay over standard HTTP(S)/WS(S) on port
   8090. They operate cleanly behind NATs, firewalls, and Kubernetes ingress
   controllers without requiring inbound ports.
2. **Failure mode mismatch**: In production, the primary failure mode is
   executor process termination or crash, not network reachability. A network
   tunnel adds operational overhead without improving executor liveness or
   recovery.
3. **Peer coordination is already solved**: Relay instances coordinate
   cross-node routing via HMAC-SHA256 signed HTTP peer forwarding
   (`/internal/v1/forward`) over existing internal infrastructure.

The only valid future application of this pattern is a potential standalone
`relay agent` proxy mode, which can be evaluated separately if edge deployment
requirements emerge.

## Consequences

- Avoids embedding heavy networking runtimes inside the Relay binary.
- Retains standard HTTP/WebSocket transport conventions.
