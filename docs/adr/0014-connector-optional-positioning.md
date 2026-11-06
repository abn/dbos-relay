---
type: Decision
title: ADR 0014 - Connector-optional positioning
description: Position Relay as an open control plane alternative after the Conductor connector left the MIT SDKs.
status: accepted
---

# ADR 0014 - Connector-optional positioning

## Status

Accepted.

## Context

DBOS has moved the Conductor client out of the MIT SDKs and into proprietary
enterprise packages. The move is no longer only on the upstream `main`
branches. It first shipped on 2026-10-15 and has continued since:

| SDK | First post-split release | Current release | Public package after the split |
|---|---|---|---|
| Go | `v1.7.0`, 2026-10-15 | `v1.8.0`, 2026-10-28 | `dbos/enterprise.go` loader only |
| Python | `3.3.0`, 2026-10-15 | `3.4.0`, 2026-10-28 | `dbos/_enterprise.py` loader only |
| TypeScript | `5.3.0`, 2026-10-15 | `5.4.0`, 2026-10-28 | `dist/src/enterprise.js` loader only |
| Java | no split | `1.2.0`, 2026-09-30 | the public `dev/dbos/transact/conductor/` tree is still present |

The Python and npm packages publish under the DBOS Conductor Software License:
`dbos-enterprise` `3.4.0` on PyPI and `@dbos-inc/dbos-enterprise` `5.4.0` on
npm. The Go loader imports `go.dbos.dev/dbos-enterprise-go`, which Relay also
does not inspect. Relay cannot inspect or functionally test any of them, and
the licence forbids using their test results to build similar software.

This closes the premise behind the project's original positioning. Relay was
presented as a zero-code-change drop-in: an unmodified application connects by
changing its Conductor URL and key. That promise is a claim about the client
half of the protocol. For a post-split SDK, the client half is now a
proprietary package that Relay cannot verify. Keeping the promise would be a
claim about someone else's code, which the project's citation rule exists to
prevent.

The move does not close the integration surface that matters most. Every one
of those releases still ships an MIT external client, `DBOSClient` in Python
and TypeScript and `dbos.Client` in Go, with workflow queries, enqueue, send,
cancel, resume, fork, rewind, queues, schedules, and application versions. The
first post-split releases are at system database schema 123; the current
releases are at schema 125. The application's durable state is still in its
own database, written by MIT migrations. That surface is the foundation for
[ADR 0015](0015-direct-mode-system-database.md).

## Decision

Relay is positioned as an **open control plane alternative for DBOS Transact
applications**, not as a drop-in replacement for the Conductor client after
the split.

The support boundary is explicit:

1. **Attached mode** remains supported for applications on SDK releases that
   still contain the public connector. The existing supported ceiling continues
   to apply: Go at most `v1.6.0`, Python at most `3.2.0`, TypeScript at most
   `5.2.11`, and Java without a ceiling. Java remains in this mode because it
   has not split; its current release is at system database schema 114.
2. **Direct mode** is the path for applications on post-split SDK releases. It
   connects Relay to the application's system database through the MIT Go
   client. It is defined in [ADR 0015](0015-direct-mode-system-database.md) and
   covers schema 123 through 125 in its first version, which includes both
   post-split cohorts.
3. **A post-split SDK release is not supported in Attached mode.** Relay does
   not claim that a proprietary connector will connect to Relay, and it does
   not build a surrogate connector as a foundation. A user who wants Attached
   mode on a post-split SDK must supply a connector they control, and the
   compatibility then belongs to that connector, not to Relay.
4. **A post-split release below the Direct schema floor is not supported by
   either mode.** None exists today. If one appears, the options are an SDK
   upgrade or a user-supplied connector. Relay does not guess at an unknown
   schema.
5. **Parity claims name a public source.** The OpenAPI documents served by the
   Conductor API, the MIT SDK sources while they remain public, the public DBOS
   documentation, and Relay's own tests are the permitted parity sources.
   Proprietary connector behaviour and proprietary server behaviour are not
   parity targets and are not tested.

The project continues to implement the Conductor executor protocol and HTTP
API for the supported Attached window. That code is not discarded. It is the
compatibility mode for existing deployments, for Java, and for users who want
live executor telemetry. It is no longer the only road.

## Consequences

- The README, the goals page, the overview, and the landing page stop promising
  that an unmodified application on a current SDK connects by changing a URL.
  They describe the two modes and the boundary between them. The textual pages
  are updated with this decision; the landing page change carries rendered
  screenshots and follows as a separate visual change.
- [Supported SDKs](../usage/supported-sdks.md) records the ceiling as the
  Attached mode boundary and the Direct mode schema window. The Direct
  implementation, recovery path, and probe are covered by the data-plane test
  suite; the multi-SDK matrix extension follows.
- Parity gaps that can only be observed by running the proprietary connector
  or server become documented gap entries. They are not open tasks someone is
  expected to close.
- The project gains a second integration mode, which must be tested and
  documented with the same rigour as the first. A mode that is not in the
  conformance matrix is not supported.
- The upstream ask narrows. The useful commitment is no longer "keep the
  connector open". It is "keep the external client MIT and keep the system
  database migrations public". That ask is recorded in the Direct mode
  decision.

## Alternatives considered

1. **Keep the drop-in claim and rely on a surrogate connector.** Rejected as a
   foundation. A surrogate is a loader-specific shim that upstream controls,
   requires an application dependency change, and must be maintained in four
   languages. It remains a user-side option, not a project promise.
2. **Fork the SDKs and keep the connector.** Rejected. A fork carries the
   security and release burden of four SDKs and creates ecosystem confusion.
3. **Do nothing and wait for upstream.** Rejected. The split has shipped. A
   plan that waits leaves the applications that have already moved without a
   supported path.
