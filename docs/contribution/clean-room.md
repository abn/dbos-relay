---
type: Reference
title: Clean-room rules
description: The permitted and forbidden sources for anything that reaches Relay.
status: stable
---

# Clean-room rules

Relay implements contracts that a commercial product also implements. It is
built only from public, permissively licensed sources. These rules bind every
contributor and every agent session.

This page is the canonical list, and the only copy. Everything else in the
project links here instead of restating it, so there is nothing to keep in
sync. The reasoning behind the approach is in [ADR
0001](../adr/0001-clean-room-derivation.md).

## Permitted sources

1. The source of the MIT-licensed DBOS Transact SDKs, in any language.
2. The source of the open-source control-plane command-line client, after
   checking its licence file. Whether it is permissively licensed is an open
   question until someone reads that file.
3. The publicly served OpenAPI documents for the Conductor HTTP API, fetched
   without authentication and without accepting a click-through licence.
4. Public documentation and public web pages published by DBOS, Inc.
5. The MIT-licensed dbos-argus project.
6. Relay's own tests, fixtures, and sample applications.

## Forbidden sources

1. The proprietary Conductor and Console container images. Never downloaded,
   never run, never used as an oracle, a screenshot, or a benchmark, including
   under any free test and development terms.
2. Any Conductor licence key.
3. The hosted Console interface, and any screenshot or recording of it.
4. Any network capture of traffic to the hosted service.
5. Any non-public material from DBOS, Inc.

Benchmark results for the proprietary implementation appear to be treated as
confidential information under its licence. Relay does not produce, quote, or
design against them regardless, so the exact wording does not change the rule.

## Provenance

Every protocol or API fact that reaches Relay's code or documentation names
its source: a repository, path, and commit, or a URL and a fetch date. A fact
without a citation is a guess, and is labelled as one until it is tested.
Vendored artefacts record their checksum and fetch date alongside the file.

Those permitted origins collect in the **provenance ledger**, a permitted source register of
public sources the project relies on. Detailed fact-level citations (commit hash,
file path, and line or pointer references) are recorded in the provenance and
clean-room citation sections of individual discovery documents.

## Derivation

Non-trivial code is not copied out of the SDKs, even though their licence
permits it. The protocol is re-derived as a written specification, and the
implementation follows that specification. Small constants such as message
type strings and field names are expected and fine.

The test that matters: a reviewer should be able to implement a test executor
from the specification alone, without opening an SDK.

## When a fact is only available from a forbidden source

Stop and surface the gap. Do not guess, and do not reach for the forbidden
source to check. Design a fallback instead: accept a superset of what the
protocol might send, or degrade gracefully, and record the open question so it
can be closed by evidence later.

## Trademarks

Relay does not use ecosystem trademarks in its own name, package names, binary
name, logo, or domain. This extends to internal module names, since a module
name becomes a package name. Describing the compatibility target in prose is
fine and unavoidable. The README states plainly that the project is not
affiliated with or endorsed by DBOS, Inc.

## Standing

These rules are engineering guidance. They are not legal advice, and they have
not been reviewed by a lawyer. Independent legal review is a blocker on the
first public release, and the licence characterisations on this page are the
project's reading of public documents, which is a weaker thing.

## Sources

The public documents this page and the wider wiki draw on, all fetched
without authentication:

* `https://docs.dbos.dev/`, the product documentation, in particular the
  architecture, conductor, hosting, API, recovery, and configuration pages
* `https://dbos.dev/conductor-license`, the licence text for the proprietary
  server
* `https://github.com/dbos-inc/`, the SDK and command-line client repositories
* `https://github.com/tmarkovski/dbos-argus`, the reusable interface package

Every fact drawn from these is re-verified during discovery, with the file,
line, commit, or fetch date recorded in the ledger. Until that happens, the
wiki marks the specific claims that rest on them.
