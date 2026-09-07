# Documentation update log

This log tracks the evolution of the knowledge base: page additions,
deprecations, and structural refactors. It does not track software releases or
implementation milestones; those belong in the commit history.

## 2026-09-08

* **Creation**: Opened the [discovery](discovery/index.md) section and its
  [provenance ledger](discovery/provenance.md) recording permitted public
  sources for clean-room derivation.
* **Creation**: Opened the [protocol](protocol/index.md) section index for the
  executor WebSocket specification and wire mappings.
* **Creation**: Added [ADR 0003](adr/0003-implementation-language.md)
  recording Go as the implementation language, which closes the stack question
  discovery item D1 was opened to answer. The record also fixes the supporting
  library choices, including the corrected import path for the WebSocket
  library, whose old path appears in older design notes.
* **Update**: Linked both discovery and protocol sections from the root
  [index](index.md).
* **Update**: [Components](architecture/components.md) now names the planned
  Go package for each module. The page still describes an intended design, and
  says so.

## 2026-09-07

* **Creation**: Opened the bundle with the operational baseline. Added the
  root index, this log, the [overview](overview.md), the design section
  ([goals](design/goals.md), [compatibility
  tiers](design/compatibility-tiers.md), [terminology](design/terminology.md)),
  the architecture section ([components](architecture/components.md),
  [recovery](architecture/recovery.md)), the first two decision records, and
  the contribution section ([guide](contribution/guide.md), [clean-room
  rules](contribution/clean-room.md), [maintainer
  guide](contribution/maintainers.md)).
* **Note**: The bundle deliberately has no usage or reference section yet.
  There is nothing to run, so a page describing how to run it would be
  fiction. Both sections open when the first binary does.
* **Note**: Every architecture page describes an intended design, not shipped
  behaviour, and says so. Pages are rewritten against the code as the code
  lands.
