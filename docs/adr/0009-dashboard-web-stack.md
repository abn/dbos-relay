---
type: Decision
title: Dashboard web stack
description: Use zero-dependency vanilla JavaScript runtime with esbuild asset bundling and bespoke SVG visualization for the embedded web dashboard.
status: accepted
---

# ADR 0009: Dashboard web stack

## Context

Relay embeds a web dashboard (served by `internal/dashboard`) for inspecting
applications, executors, workflows, and step executions. The initial exploration
considered heavy framework baselines (such as SvelteKit) and automated OpenAPI
type generation.

However, complex client-side framework runtimes introduce runtime overhead,
supply-chain risks, and unnecessary complexity for Relay's single-binary embed
model.

## Decision

Relay implements the web dashboard as a lightweight vanilla JavaScript client with an esbuild asset compilation step:

1. **Runtime framework**: Vanilla JavaScript in the browser using standard DOM templates and native browser APIs, with zero runtime UI framework dependencies.
2. **Build and bundling**: Uses `esbuild` as a development dependency via `node build.js` to bundle modular client scripts into a single standalone IIFE distribution bundle (`internal/dashboard/dist/assets/app.<hash>.js`), verified with `node --check`. The bundle and stylesheet carry a content hash in the file name, so the immutable cache headers served for `assets/` stay correct across upgrades; `index.html` is rewritten against the new names at build time.
3. **DAG visualization**: Bespoke SVG renderer (`WorkflowDAG.js`) tailored to DBOS workflow step graphs.
4. **Styling**: Hand-written CSS without external utility frameworks.
5. **Types**: Client models in `src/lib/api/types.ts` are maintained directly against Relay OpenAPI schemas rather than generated via heavy external node tooling.

## Consequences

- The client runtime requires zero external framework libraries in the user's browser.
- Building dashboard assets from source requires Node.js and `esbuild` (configured as a devDependency in `package.json`).
- Pre-built distribution assets are checked into `internal/dashboard/dist` and embedded via Go's `//go:embed`, allowing standard Go binary builds to succeed without requiring Node or npm on the host.
- TypeScript client types in `src/lib/api/types.ts` are maintained manually when the API contract changes.
