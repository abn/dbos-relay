---
type: Guide
title: Maintainer guide
description: The review gate, ground truth, and keeping the wiki honest.
status: draft
---

# Maintainer guide

## The review gate

Every change is reviewed as a skeptical maintainer before it is pushed. The
role card in `.agents/agents/reviewer.md` describes the gate for an agent
session; the criteria are the same for a person.

- Does the change satisfy its stated scope, and is it tightly scoped as the
  smallest clean change that does so?
- Does it maintain a single source of truth and avoid duplication or
  divergence?
- Is every committed file and commit message free of internal process,
  tracking, task, or work identifiers?
- Does every new protocol or API claim cite a permitted source, or is it
  proven by a test against a real SDK?
- Does it respect the invariants: clean-room sourcing, no access to an
  application's database, no application-side change required, parity with the
  documented behaviour?
- Do the tests assert real behaviour, and does the wiki reflect what the code
  now does?

A review returns either a pass or a list of concrete issues with a proposed
fix. The human holds the final decision to push.

## Ground truth

When a question touches the protocol or the API, the order of authority is:

1. **The SDK source**, for the executor protocol. It is the client half of the
   contract and it is the only complete description that exists in public.
2. **The published OpenAPI document**, for the HTTP surface, vendored with its
   checksum and fetch date.
3. **The public documentation**, for behaviour the first two do not pin down,
   such as timing defaults and operational semantics.
4. **A test against a real SDK**, when the first three disagree or are silent.

The proprietary implementation is never consulted, for any of these. See
[clean-room rules](clean-room.md).

Where the SDKs disagree with each other, Relay accepts the union of what they
send and requires nothing that is specific to one of them.

## Keeping the wiki honest

The wiki reflects status quo. When the code and a page disagree, the code
wins, and the correction is recorded in [the log](../log.md). Pages
that describe an intended design say so in plain words, so a reader never
mistakes a plan for a shipped feature.

The internal progress log is not part of the wiki. It lives in the scratch
area under `.agents/brain/outbox/`.
