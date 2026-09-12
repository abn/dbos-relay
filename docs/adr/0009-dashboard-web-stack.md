---
type: Decision
title: Dashboard web stack
description: Use zero-dependency vanilla JavaScript and bespoke SVG visualization for the embedded web console.
status: accepted
---

# ADR 0009: Dashboard web stack

## Context

Relay embeds a web dashboard (`console/`) for inspecting applications,
executors, workflows, and step executions. The initial exploration considered
framework baselines (such as SvelteKit) and automated OpenAPI type generation.

However, adding npm framework dependencies introduces build complexity, supply-chain
risks, and version drift against Relay's single-binary embed model.

## Decision

Relay implements the web dashboard as a zero-dependency vanilla JS application:

1. **Framework**: Vanilla JavaScript using DOM templates and native browser APIs, with no npm dependencies.
2. **DAG visualization**: Bespoke SVG renderer (`WorkflowDAG.js`) tailored to DBOS workflow step graphs.
3. **Styling**: Hand-written CSS without external utility frameworks.
4. **Types**: Client models in `console/src/lib/api/types.ts` are maintained manually against Relay OpenAPI schemas rather than generated via external node tooling.

## Consequences

- The build requires no external npm packages or node toolchains.
- TypeScript client types in `console/src/lib/api/types.ts` are maintained manually when the API contract changes.
