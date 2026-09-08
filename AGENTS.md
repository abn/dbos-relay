# AGENTS.md

Operational contract for humans and agents working in this repository.

## Project

Relay is an open-source control plane for DBOS Transact applications,
compatible with the Conductor executor WebSocket protocol and HTTP API. It is
self-hostable, MIT licensed, and not affiliated with or endorsed by DBOS, Inc.

The project is in Phase 0: discovery. No server code is written until the
discovery documents are accepted. The working specification is held in the
scratch area, which is gitignored and never committed. The public design lives
in the wiki under `docs/`.

## Invariants

- **Clean-room only.** Relay is derived from permitted public sources only.
  The proprietary Conductor and Console images are never downloaded, run,
  observed, or benchmarked. The canonical list of permitted and forbidden
  sources is [the clean-room rules](docs/contribution/clean-room.md). Read it
  before acting; it binds every session and is not summarised here, because a
  summary is how a source list goes stale.
- **Cite or test.** Every protocol or API claim written into code or docs
  names its permitted source, or has a test that proves it against a real SDK.
  An uncited claim is a guess and is labelled as one.
- **Ask when provenance is unclear.** If a fact is only obtainable from a
  forbidden source, stop and surface the gap. Design a fallback rather than
  guessing.
- **No direct application database queries.** Relay never speaks to an
  application's system database except through the SDK's own client library,
  and only for applications where the operator has explicitly configured a
  data-plane connection. Raw SQL against `dbos.*` is still forbidden.
- **Never require application-side changes.** An unmodified DBOS application
  must work by pointing its Conductor URL at Relay. If a feature needs an SDK
  change, file it upstream and keep Relay working without it.
- **Parity over preference.** When "nicer" and "identical to upstream
  behaviour" conflict, choose identical unless an ADR records why not.
- **No trademark use in identity.** Relay does not use "DBOS" or "Conductor"
  in its project name, package names, binary name, module names, logo, or
  domain. A module name becomes a package name, so the rule reaches inside the
  codebase. Descriptive use in prose is fine.
- **Avoid duplication and divergence.** Maintain a single source of truth for
  all code, schemas, wire types, models, and documentation. Look out for and
  eliminate duplicate structures or parallel logic across packages. Guard
  against divergence: keep protocol implementations strictly aligned with
  upstream contracts, avoiding subtle drift or shadow representations.
- **Tightly scoped changes.** Every change must be minimal and strictly
  bounded to its stated scope: unit of work, target, and outcome. Avoid
  opportunistic refactoring, speculative abstractions, feature creep, or
  incidental edits in unrelated files. If a separate issue is spotted, handle
  it in a distinct change.
- **No internal process leaks.** All committed files (code, tests, schemas,
  documentation, comments, and commit messages) must avoid internal references
  to process, tracking, task IDs, work identifiers, ticket numbers, scratch
  paths (`.agents/brain/`), wave or lane designations, or agent operational
  notes. Internal tracking belongs in the scratch area only; the committed
  repository represents a clean public project.
- **Always-public-ready docs.** `docs/` is an OKF v0.2 bundle. No internal
  names, codenames, hostnames, absolute paths, tokens, or task identifiers.
- **No AI slop.** No em-dashes, no marketing fluff, no filler prose, no
  comments that restate the code. Code and docs read like a human wrote them.
- **No sudo** in this repository unless a human explicitly requests elevation.

## Automation and conventions

- **Prefer automation over manual conformance.** `pre-commit` and `make check`
  own style, conventions, and quality gates. Do not hand-polish what a tool
  can enforce.
- **Tool-specific assets stay out of the repository.** Assistant shims and the
  scratch area under `.agents/brain/` are git-ignored. `.agents/bootstrap.sh`
  regenerates the shims; it is idempotent and safe to re-run.
- **Makefile** is the single automation entrypoint. Bare `make` shows the
  targets. `make check` is the gate that hooks and CI both reuse.
- **Commits** follow Conventional Commits, summary first, no trailers (no
  Co-Authored-By, no Signed-off-by). Stage explicit paths, never `git add -A`.
- **Changes** happen in a dedicated worktree with a conventional branch name
  (`feat/`, `fix/`, `docs/`, `chore/`, `refactor/`), rebased on latest `main`.
- **Clean history.** Fix up or amend into the owning commit on active
  branches rather than stacking fix commits.
- **Scope discipline.** State the scope in one sentence before starting:
  unit of work, target, outcome. Anything not needed for that outcome is out
  of scope.

## Verification

A change is not done until `make check` passes and the relevant tests are
green, with real captured output. Behaviour changes update the wiki, and
wiki changes are recorded in `docs/log.md`.

## Contributor guide

See the [contributor guide](docs/contribution/guide.md), the [clean-room
rules](docs/contribution/clean-room.md), and for maintainers the [maintainer
guide](docs/contribution/maintainers.md).
