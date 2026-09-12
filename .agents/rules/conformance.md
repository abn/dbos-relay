# Conformance rules

Rules governing protocol parity, specification alignment, and conformance drift
refinement for Relay.

## Parity over preference

- Parity with the upstream Conductor specification and DBOS Transact executor
  protocol takes precedence over internal preferences or cleaner alternative
  designs.
- If upstream defines a status code, field name, envelope format, or query
  parameter, Relay must match it exactly.
- Deviations or additions require an Architectural Decision Record (ADR)
  explaining why parity cannot or should not be maintained.

## Clean-room derivation

- All conformance claims and test assertions must be derived solely from
  permitted clean-room sources listed in `docs/contribution/clean-room.md`.
- The proprietary Conductor server image is never downloaded, run, observed, or
  benchmarked, even to verify test cases.
- If a protocol detail cannot be proven from permitted sources, write an
  isolated blackbox test against a permitted SDK client to verify the behavior.

## Zero divergence in wire types

- Maintain a single source of truth for protocol messages in `internal/protocol/`.
- Do not introduce shadow representations or loose map interfaces for wire
  messages.
- All message structs must include provenance comments citing the SDK file and
  commit hash.
- Casing of JSON keys must strictly match upstream conventions, accounting for
  language SDK differences where upstream permits them.

## Route gating in self-hosted mode

- Upstream Conductor returns HTTP 404 (not 403) for OAuth-gated endpoints when
  operating in self-hosted mode without an identity provider.
- Relay must maintain this exact route-gating behavior in `internal/api/` to
  prevent upstream CLI tooling (`dbosctl`) from failing unexpectedly.

## Test-first drift refinement

- When conformance drift is discovered, write a failing test in
  `tests/conformance/` or `internal/conformance/` before changing production
  code.
- The test must reproduce the exact discrepancy against the contract.
- Fixes must be minimal and strictly bounded to the identified drift. Do not
  bundle opportunistic refactoring, dependency upgrades, or unrelated schema
  cleanup.

## Quality gate

- A conformance change is not complete until:
  1. `make test/conformance` passes cleanly.
  2. `make check` passes with zero linter issues and no drift.
  3. Documentation in `docs/` is updated if observable behavior changed.
  4. Changes are recorded in `docs/log.md`.
