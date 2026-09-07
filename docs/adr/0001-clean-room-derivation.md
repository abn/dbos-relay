---
type: Decision
title: ADR 0001 - Clean-room derivation from permitted sources
description: Build only from public, permissively licensed sources, and never from the proprietary implementation.
status: stable
---

# ADR 0001 - Clean-room derivation from permitted sources

## Status

Accepted. This decision predates any code and constrains all of it.

## Context

The contracts Relay implements are already public. The SDKs that speak the
executor protocol are MIT licensed and contain the whole client side of it.
The HTTP API is published as an OpenAPI document which, on the evidence of the
public documentation, is served without authentication. The command-line
client that consumes it is open source, though its exact licence is an open
question until someone reads the file. Both of those are confirmed during
discovery before anything depends on them.

The server that sits between them is proprietary, and its licence appears to
prohibit reverse engineering, to prohibit using confidential information to
design similar software, and to treat benchmark results as confidential. Free
test and development use appears to be available, which is what makes running
it tempting as an oracle. These readings come from the public licence page and
have not been reviewed by a lawyer; the decision below does not depend on
which of them is exactly right.

Taking that path would be the single fastest way to make this project
indefensible. The value of a compatible control plane is that people can
deploy it without worrying about where it came from, and that value does not
survive an ambiguous provenance story.

## Decision

Relay is derived only from permitted sources. The canonical list of what is
permitted and what is forbidden lives in [the clean-room
rules](../contribution/clean-room.md), which is the single copy in the project.

The proprietary server and console images are never downloaded, run, observed,
or benchmarked, at any point, for any reason, including testing.

Two rules follow from that and are enforced rather than trusted:

- **Cite or test.** Every protocol or API fact reaching code or docs names its
  source, or is proven by a test against a real SDK. An uncited fact is a
  guess and is marked as one.
- **Re-derive, do not copy.** Non-trivial code is not copied from the SDKs
  even though MIT permits it. The protocol is written down as a specification
  first, and the implementation follows the specification. Small constants
  such as message type strings and field names are expected.

When a fact is only obtainable from a forbidden source, the gap is surfaced
and a fallback is designed. Relay accepts a superset or degrades gracefully
rather than guessing.

## Consequences

- Development is slower. There is no oracle to diff against, so the protocol
  specification and the conformance suite carry weight that a reference
  implementation would otherwise carry.
- The specification is a first-class deliverable, not documentation of the
  code. It is written so that a reviewer can implement a test executor from it
  without opening an SDK, which is also what makes it reviewable.
- Testing is done against real SDK sample applications and a test double, both
  of which are permitted inputs.
- The rules are written into `AGENTS.md` and the [clean-room
  rules](../contribution/clean-room.md) because they bind agent sessions as
  much as people, and an agent will otherwise reach for the fastest source.
- The hooks cannot enforce this one. A pattern match cannot tell where an idea
  came from, so provenance is checked by the reviewer on every change, and the
  provenance ledger is the only durable evidence.
- This is engineering guidance. It is not legal advice, and independent legal
  review is a blocker on the first public release.
