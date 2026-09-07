---
type: Decision
title: ADR 0002 - MIT licence and naming constraints
description: License under MIT and keep ecosystem trademarks out of the project identity.
status: draft
---

# ADR 0002 - MIT licence and naming constraints

## Status

Partly decided. The licence is settled. The final project name is not, and
this record is updated when it is chosen.

## Context

Relay is compatible with a commercial product, in an ecosystem whose client
libraries are MIT licensed and whose vendor also maintains the SDKs the
project depends on. The vendor should be able to read this project as
complementary to theirs, and both the licence and the name affect whether it
looks that way.

Trademarks are a separate matter from copyright. Using a product name
descriptively is ordinary; using it as your own project, package, or binary
name is not.

## Decision

License under MIT, matching the SDKs and the other open-source projects in
this space. It is the least friction for anyone adopting the result and it
carries no obligations that would discourage the vendor from treating the
project as complementary.

Keep ecosystem trademarks out of the project's identity: not in the project
name, package names, binary name, logo, or domain. Describing what Relay is
compatible with in prose is fine and necessary, and the README carries a plain
statement that the project is not affiliated with or endorsed by the vendor.

"Relay" is a working name. Before the first public release the name is checked
for collisions across the package registries, container registries, and
domains that matter, and a more distinctive variant is chosen if needed. The
local repository directory is not the project name and does not constrain it.

## Consequences

- Anyone can fork, embed, or commercialise Relay. That is the intended
  outcome.
- Naming is a release blocker. The name appears in a binary, a module path,
  and a container image, and changing it after publication is expensive.
- Prose in the wiki names the compatibility target directly, which is
  descriptive use and is deliberate. The project's own identity does not.
