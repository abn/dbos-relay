---
type: Reference
title: Supported SDKs
description: The Attached mode ceiling, the Direct mode schema window, and the checks for each.
status: draft
---

# Supported SDKs

Relay has two integration modes with different support boundaries.

**Attached mode** speaks the Conductor executor WebSocket protocol. It is
bounded by the SDK releases that still contain the public connector. The
connector has left the MIT SDKs, so the boundary is now a wall rather than a
temporary ceiling.

**Direct mode** connects to the application's system database through the
official MIT Go client. It is bounded by the system database schema, not by the
SDK version. It is the path for applications on post-split SDK releases. See
[ADR 0015](../adr/0015-direct-mode-system-database.md).

Direct mode is implemented for the supported schema window. The engine and
recovery path are covered by hermetic SQLite tests and live PostgreSQL
data-plane tests; the probe is covered by liveness tests. Python and TypeScript
cross-language coverage lives in `tests/direct`.

The two boundaries are recorded here, with the evidence behind each one.

## Support matrix

| SDK | Attached mode | Direct mode |
|---|---|---|
| Go | `v1.6.0` and below | schema 123 through 125, through the Go client |
| Python | `3.2.0` and below | schema 123 through 125, through the Go client |
| TypeScript | `5.2.11` and below | schema 123 through 125, through the Go client |
| Java | no ceiling yet; `1.2.0` is schema 114 | not yet; Java schema 114 uses Attached mode |

The Attached ceiling is an upper bound, not a recommendation. Any version at or
below it works, including versions that predate it. An unmodified application
built against a supported Attached SDK connects to Relay by pointing its
Conductor URL at Relay and needs no code change, no configuration change, and
no Relay-side setting.

The Direct window is a schema window, not an SDK window. Both post-split
cohorts are covered: schema 123 (Go `v1.7.0`, Python `3.3.0`, TypeScript
`5.3.0`) and schema 125 (Go `v1.8.0`, Python `3.4.0`, TypeScript `5.4.0`).
Newer schemas are tolerated by the client's migration check until the pin is
raised, but they are not claimed as tested. A schema below 123 is refused and
the application uses Attached mode.

## Why the Attached ceiling exists

Three SDKs removed the Conductor connector from the public package and moved it
into a proprietary enterprise package. The split first shipped on 2026-10-15:

| SDK | First post-split release | Current release | Public package after the split |
|---|---|---|---|
| Go | `v1.7.0` | `v1.8.0` | `dbos/enterprise.go` loader only |
| Python | `3.3.0` | `3.4.0` | `dbos/_enterprise.py` loader only |
| TypeScript | `5.3.0` | `5.4.0` | `dist/src/enterprise.js` loader only |

The Python and npm packages publish under the DBOS Conductor Software
License: `dbos-enterprise` `3.4.0` on PyPI and `@dbos-inc/dbos-enterprise`
`5.4.0` on npm. The Go loader imports `go.dbos.dev/dbos-enterprise-go`, which
Relay also does not inspect. Relay cannot test any of them, so it cannot promise
that a post-split SDK connects to Relay in Attached mode.

Java has no split. At the latest release, `1.2.0`, the whole
`transact/src/main/java/dev/dbos/transact/conductor/` tree is still present and
public. Java `1.2.0` is at system database schema 114. Java is the only SDK
that still publishes the wire protocol in the open, which is why it keeps an
Attached ceiling of none and why its connector is watched separately.

## Why Direct mode exists

The split removed the transport, not the durable state or the external client.
Every one of those releases still ships an MIT external client, `dbos.Client`
in Go and `DBOSClient` in Python and TypeScript, with workflow queries, enqueue,
send, cancel, resume, fork, rewind, queues, schedules, and application
versions. Both post-split cohorts are on known schema generations: 123 and 125.
The Go SDK `v1.7.0` requires schema 123 and tolerates newer, supports
PostgreSQL, and provides the MIT `dbos/internals` seam that exposes
`SystemDatabase.ReenqueueForRecovery`. The SDK core runs the database-backed
scheduler in a launched application; the external client creates and manages
the schedule rows.

Direct mode uses that client against the application's own database. It does
not need the connector, an enterprise package, or a vendor key. The
application still needs no code change; the operator supplies a database
connection and, for automatic recovery, a liveness signal.

## Evidence for the Attached ceiling

Every version claim below names the repository tag it comes from and the
package-registry upload date for the same version. Registry timestamps are UTC;
repository tag dates are the committer dates of the tagged commits.

### Go

* Tag `v1.6.0` in `dbos-inc/dbos-transact-golang`, commit
  `04337c15667adcdf5cc057be448ed9545d6f06d9`, tagged 2026-10-07. It is the last
  release whose published module contains `dbos/conductor.go` and
  `dbos/conductor_protocol.go`.
* Tag `v1.7.0`, commit `339705771d7e198c5bac9e213903820c0575e6c1`, published
  2026-10-15T17:53:57Z. Its module contains `dbos/enterprise.go` and no
  Conductor client; its migration list ends at 123.
* Tag `v1.8.0`, commit `f543516f2014d0c3d80a08dbe44fcc545c76a183`, published
  2026-10-28T01:25:24Z. Its migration list ends at 125.

### Python

* Tag `3.2.0` in `dbos-inc/dbos-transact-py`, commit
  `d324156616281d7d4923bc60851617bdbb70ea5e`, tagged 2026-09-28. PyPI reports
  `dbos` `3.2.0` uploaded `2026-09-29T15:43:49`, and it is the last release
  whose wheel contains `dbos/_conductor/`.
* Tag `3.3.0`, commit `61426cb95bcd749dd18137b4eb483d02b9975d7d`, uploaded
  `2026-10-15T17:20:58Z`. Its wheel contains `dbos/_enterprise.py` and no
  Conductor client; its migration list ends at 123.
* Tag `3.4.0`, commit `718d06613d9b7dc4c935e098e4608b6745267760`, uploaded
  `2026-10-28T15:25:33Z`. Its migration list ends at 125.

### TypeScript

* Tag `v5.2` in `dbos-inc/dbos-transact-ts`, commit
  `3f36908f58fd8b3079cbf5372203c7a2d0ba06cc`, tagged 2026-09-29. The npm
  registry reports `@dbos-inc/dbos-sdk` `5.2.11` published
  `2026-09-29T15:48:15.957Z`, and it is the last stable release whose tarball
  contains `dist/src/conductor/`.
* Tag `v5.3`, commit `8643d389db64351f2acfce45e96e9747b4be8465`, published
  `2026-10-15T17:22:51Z`. Its tarball contains `dist/src/enterprise.js` and no
  Conductor client; its migration list ends at 123.
* Tag `v5.4`, commit `a70b65f93e5cf282014b7606a9db91b00231d753`, published
  `2026-10-28T15:30:11Z`. Its migration list ends at 125.

### Java

* The latest release tag is `1.2.0`, commit
  `0f4fe187b20bd6f5f735d8f99d810aeb90b8314d`, tagged 2026-09-30. It carries
  `MINIMUM_SYSDB_VERSION` 111, a migration list ending at 114, and the full
  `transact/src/main/java/dev/dbos/transact/conductor/` tree with no enterprise
  package anywhere in the tree.
* Because there is no split commit, there is nothing to cap yet. A Java split
  would remove the last public copy of the wire protocol and would make Java a
  Direct mode candidate once its schema aligns with the Direct window.

### Protocol pins

The protocol facts in this wiki are pinned in the [provenance
ledger](../discovery/provenance.md), which records the repository, commit, and
confirmation date for each SDK source. Those pins bound what Relay's protocol
description was derived from. They are separate from the Attached ceiling,
which bounds what an application may run.

## Enforcement

The data-plane suite includes a hermetic SQLite test against the real client
and live PostgreSQL tests. The multi-SDK matrix extension follows.

* `tests/verifysdk/sdk_ceiling_test.go` reads the manifests of the sample
  applications in `examples/` and fails when a pinned Attached SDK version is
  above the ceiling recorded in this page. It reads files only, runs offline,
  and is part of `make test`.
* `make watch/java-connector` runs `scripts/watch-java-connector.py`, which
  clones the Java SDK and fails when the Conductor connector package is no
  longer present at the recorded baseline. It needs network access, so it is
  not part of `make check`; run it on a schedule or before a release.
* The Direct mode tests run the real client against schema 123 and exercise
  read, control, schedule, and recovery without a connector. The cross-language
  suite runs Python `3.4.0` and TypeScript `5.4.0` at schema 125 against
  PostgreSQL, and Python against SQLite without a server.

## Renewing a boundary

For Attached mode:

1. Re-verify the upstream state: the newest tag of each SDK, the newest
   registry upload, and whether the connector is still in that published
   artefact.
2. Update this page, including the citations, and record the change in the
   [documentation log](../log.md).
3. Update the constants in `tests/verifysdk/sdk_ceiling_test.go` so the
   example pins are checked against the new bound.
4. Keep the Attached sample applications at or below the new bound, and move
   any example that crosses it to Direct mode with the matching schema. Confirm
   each still builds.
5. If the Java connector has moved, update the baseline in
   `scripts/watch-java-connector.py` and record the new last-known-good commit
   and file count.

For Direct mode:

1. Re-verify the schema version and client requirement of the current Go,
   Python, and TypeScript releases, and confirm that the external client is
   still MIT.
2. Add a schema fixture and a conformance cell for the new generation.
3. Update the window in this page and the constants in the Direct mode test.
4. Remove the oldest generation only when no supported example depends on it.
