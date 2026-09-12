---
type: Guide
title: Contributor guide
description: Conventions, workflow, and verification for a first change.
status: draft
---

# Contributor guide

The operational contract lives in `AGENTS.md`. This is the human-readable
how-to for making a change.

Read the [clean-room rules](clean-room.md) first. They constrain where
information may come from, and a change built on the wrong source cannot be
merged however good it is.

## Setup

```
./.agents/bootstrap.sh
make check
```

The bootstrap script installs the git hooks and writes the local assistant
shims. It is idempotent, so re-run it whenever hooks or tooling change.

## Development targets

- `make build`: compiles `bin/relay`
- `make test`: runs the unit and integration tests
- `make vet`: runs static analysis and golangci-lint
- `make drift`: verifies sqlc and OpenAPI code generation is up to date
- `make check`: runs pre-commit hooks, linters, tests, and drift check
- `make db/up`, `make db/down`: manages the local test database

## The workflow

1. **State the scope in one sentence** before starting: the unit of work, the
   target, and the outcome. Anything outside it is out of scope.
2. **Work in a dedicated worktree** with a conventional branch name (`feat/`,
   `fix/`, `docs/`, `chore/`, `refactor/`), rebased on the latest `main`.
3. **Keep the change small.** One logical unit. Fix up or amend into the
   owning commit rather than stacking fix commits on an active branch.
4. **Write the tests and update the wiki.** A behaviour change that leaves the
   docs stale is not finished.
5. **Run `make check`** and keep the output. A change is done when the gate
   passes and you have the captured output to show for it.

## Conventions

- **Commits** follow Conventional Commits, summary first, no trailers. Stage
  explicit paths, never `git add -A`. The commit-message hook enforces this.
- **Verification is automated.** `pre-commit` owns file hygiene, commit
  message shape, and the editorial and privacy patterns. `make check` runs the
  same hooks CI runs. Do not hand-polish what a tool enforces.
- **Avoid duplication and divergence.** Maintain a single source of truth for
  all code, schemas, and models. Do not introduce parallel types or duplicate
  logic, and keep implementations faithful to upstream contracts.
- **Committed files are public-ready.** Committed code, tests, documentation,
  and commit messages must never reference internal tracking, task IDs, work
  identifiers, wave or lane names, or scratch area paths. Internal process
  stays in the gitignored scratch area.
- **Docs are public-ready by default.** `docs/` is an OKF v0.2 bundle: no
  internal names, hostnames, absolute paths, tokens, or task identifiers.
  Links are relative or rooted at the bundle; no `file://` and nothing
  pointing outside `docs/`. Wiki changes are recorded in [the log](../log.md).
- **The scratch area is local.** Working notes, task breakdowns, and the
  internal progress log live in the gitignored scratch area and are never committed.

## Discovery before code

Discovery and specification work precede implementation. If an implementation
exposes a gap in the protocol specification, the specification is fixed first
and the code follows.

## Related

- [Clean-room rules](clean-room.md) - permitted and forbidden sources
- [Maintainer guide](maintainers.md) - the review gate
- `AGENTS.md` - the authoritative operational contract
