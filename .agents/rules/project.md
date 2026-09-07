# Relay project rules

Project-specific rules that extend the global standards. `AGENTS.md` is the
contract; this file is the operating detail behind it.

## Discovery before code

- Phase 0 discovery documents come first. No server code is written until the
  stack decision, the executor protocol specification, the vendored REST
  contract, the permission model, the client behavioural notes, and the
  interface reuse assessment are written and reviewed.
- If a later phase reveals a gap in the protocol specification, fix the
  specification first, then write the code.

## Provenance

- Every protocol or API fact that reaches code or docs cites a permitted
  source: repository, path, and commit, or URL and fetch date. Facts without a
  citation are treated as guesses and must be tested, not assumed.
- Vendored artefacts record their SHA-256 and fetch date next to the file.

## Scope discipline

- Before any work, state the scope in one sentence: unit of work (feature, bug
  fix, or maintenance), target, and outcome. Anything not needed for that
  outcome is out of scope by default.
- Never widen a diff to look more thorough. The smallest change that fully and
  correctly satisfies the scope, with passing tests and honest docs, is the
  target.

## Worktrees and history

- Every scoped change gets its own worktree with a conventional branch name
  (`feat/`, `fix/`, `docs/`, `chore/`, `refactor/`), rebased on latest `main`.
- Commit messages follow Conventional Commits, summary first, no trailers.
  Fix up or amend into the owning commit on active branches.
- Stage explicit paths. Never `git add -A`.

## Compatibility

- Behaviour is measured against the published contract, not against taste.
  Copy the specification's name constraints, error shapes, and status codes
  exactly, because generated clients validate against them.
- Every executor-served request has a deadline and maps failure to the status
  code the specification defines. No caching of workflow data.

## Verification gate

- A change is done only when `make check` passes and the relevant tests are
  green, with real captured output. Wiki changes are reflected in
  `docs/log.md`.
- The internal progress log lives in `.agents/brain/outbox/` and never in the
  wiki.
