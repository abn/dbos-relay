---
type: Reference
title: Supported SDKs
description: The supported executor SDK range, why it has a ceiling, and how the ceiling is enforced.
status: stable
---

# Supported SDKs

Relay speaks the Conductor executor WebSocket protocol, and it derives that
protocol from the public source of the DBOS Transact SDKs. Upstream is moving
the Conductor connector out of the MIT-licensed SDK packages and into
proprietary ones. Once a connector is proprietary, Relay cannot read it, so the
protocol facts it published stay readable but stop being verifiable. This page
records the range of SDK versions Relay supports and the evidence behind it.

## Supported range

| SDK | Supported up to | Connector in the public package |
|---|---|---|
| Go | `v1.6.0` | yes, `dbos/conductor.go`, `dbos/conductor_protocol.go` |
| Python | `3.2.0` | yes, `dbos/_conductor/` |
| TypeScript | `5.2.11` | yes, `src/conductor/` |
| Java | no ceiling yet | yes, 64 files under `transact/src/main/java/dev/dbos/transact/conductor/` |

These are upper bounds, not recommendations. Any version at or below the
ceiling works, including versions that predate it. An unmodified application
built against a supported SDK connects to Relay by pointing its Conductor URL
at Relay and needs no code change, no configuration change, and no Relay-side
setting.

The ceiling is policy, not a technical limit. Relay is not blocked from
talking to a newer SDK; it is blocked from *verifying* what that SDK does, and
the project's rule is that a protocol claim is either cited from a permitted
source or proven by a test against a real SDK. The ceiling is revisited whenever
upstream publishes, which is what the checks described below are for.

## Why the ceiling exists

Three SDKs have already split the connector out of the public package on their
`main` branches. Those commits are unreleased as of 2026-10-11, so the published
releases still contain the connector.

| SDK | Split commit | Effect |
|---|---|---|
| Go | `0fa3f0dcf1a4611f15a94be9086721e4357235da`, "DBOS enterprise" (#498), 2026-10-08 | deletes `dbos/conductor.go`, `dbos/conductor_protocol.go`, and their tests |
| Python | `03fb5c92cde8e107ff333cd2b419f65e02719e99`, "Move Conductor Client" (#873), 2026-10-01 | deletes `dbos/_conductor/conductor.py`, `dbos/_conductor/protocol.py`, and `tests/test_conductor.py` |
| TypeScript | `3b7a8b28e31cb2ed44084d8a6eff006f1fb16e48`, "Move Conductor Client" (#1394), 2026-10-01 | deletes `src/conductor/conductor.ts`, `src/conductor/protocol.ts`, and the protocol test |

Java has no such commit. At commit
`85b3f0ed0458b72358fb7554bc20931bd61e7897` on `main` the whole
`transact/src/main/java/dev/dbos/transact/conductor/` tree is still present and
public, which makes Java the only SDK left that publishes the wire protocol in
the open. That is why the loss of the Java connector is watched separately, and
why it is worth reacting to quickly rather than at the next release review.

## Evidence for each ceiling

Every version claim below names the repository tag it comes from and the
package-registry upload date for the same version. Registry timestamps are UTC;
repository tag dates are the committer dates of the tagged commits.

### Go

* Tag `v1.6.0` in `dbos-inc/dbos-transact-golang`, commit
  `04337c15667adcdf5cc057be448ed9545d6f06d9`, tagged 2026-10-07.
* `proxy.golang.org` reports `@latest` as `v1.6.0` at
  `2026-10-07T16:06:46Z`.
* `git ls-tree -r v1.6.0` lists `dbos/conductor.go` and
  `dbos/conductor_protocol.go`, so the connector is still in the published
  module.

### Python

* Tag `3.2.0` in `dbos-inc/dbos-transact-py`, commit
  `d324156616281d7d4923bc60851617bdbb70ea5e`, tagged 2026-09-28.
* PyPI reports `dbos` `3.2.0` uploaded `2026-09-29T15:43:49`, and it is the
  latest release on the index.
* The published `3.2.0` distribution contains `dbos/_conductor/conductor.py`
  and `dbos/_conductor/protocol.py`.

### TypeScript

* Tag `v5.2` in `dbos-inc/dbos-transact-ts`, commit
  `3f36908f58fd8b3079cbf5372203c7a2d0ba06cc`, tagged 2026-09-29.
* The npm registry reports `@dbos-inc/dbos-sdk` `5.2.11` published
  `2026-09-29T15:48:15.957Z`, and `latest` points at `5.2.11`.
* The published `5.2.11` tarball contains `dist/src/conductor/conductor.js`
  and `dist/src/conductor/protocol.js`.
* The `5.3.x` line exists on the registry only as `preview` and `test`
  dist-tags, so no stable release has shipped past the ceiling.

### Java

* `origin/main` of `dbos-inc/dbos-transact-java` is at
  `85b3f0ed0458b72358fb7554bc20931bd61e7897`, dated 2026-10-08, and carries 64
  files under `transact/src/main/java/dev/dbos/transact/conductor/` with no
  enterprise package anywhere in the tree.
* The latest release tag is `1.2.0`, commit
  `0f4fe187b20bd6f5f735d8f99d810aeb90b8314d`, tagged 2026-09-30. Maven Central
  also serves `1.3.0-m1` through `1.3.0-m7` milestones.
* Because there is no split commit, there is nothing to cap yet.

### Protocol pins

The protocol facts in this wiki are pinned in the [provenance
ledger](../discovery/provenance.md), which records the repository, commit, and
confirmation date for each SDK source. Those pins predate the ceiling and are
not bumped by it: the ceiling bounds what an application may run, while the
pins bound what Relay's protocol description was derived from. A ceiling bump
and a pin refresh are separate changes.

## Enforcement

Two checks keep the ceiling from going stale silently.

* `tests/verifysdk/sdk_ceiling_test.go` reads the manifests of the sample
  applications in `examples/` and fails when a pinned SDK version is above the
  ceiling recorded in this page. It reads files only, runs offline, and is part
  of `make test`.
* `make watch/java-connector` runs `scripts/watch-java-connector.py`, which
  clones the Java SDK and fails when the Conductor connector package is no
  longer present at the recorded baseline. It needs network access, so it is
  not part of `make check`; run it on a schedule or before a release.

## Bumping the ceiling

1. Re-verify the upstream state: the newest tag of each SDK, the newest
   registry upload, and whether the connector is still in that published
   artefact.
2. Update this page, including the citations, and record the change in the
   [documentation log](../log.md).
3. Update the constants in `tests/verifysdk/sdk_ceiling_test.go` so the
   example pins are checked against the new bound.
4. Bump the sample application pins in `examples/` and confirm each still
   builds. The data-plane client in the root `go.mod` is deliberately not
   bumped with them; see [ADR 0004](../adr/0004-data-plane-via-sdk-client.md).
5. If the Java connector has moved, update the baseline in
   `scripts/watch-java-connector.py` and record the new last-known-good commit
   and file count.
