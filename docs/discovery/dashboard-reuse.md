---
type: Decision
title: Dashboard component reuse evaluation
description: Technical evaluation and Go or No-Go decision on consuming the @dbos-argus/ui component package for the Relay dashboard.
status: decided
---

# Dashboard component reuse evaluation

Relay plans a self-hosted web dashboard (`console/`) embedded into the
Relay binary to provide visibility into applications, executors, workflows,
steps, and queues. This document evaluates whether Relay should consume the
external `@dbos-argus/ui` package from the `dbos-argus` project or build first-party
dashboard components directly against Relay's vendored OpenAPI specifications.

## Provenance and clean-room citation

All findings in this document are derived from the following permitted sources:
* Repository: `https://github.com/tmarkovski/dbos-argus`
* Commit: `53cf15bdbea0b68f8ac2dd1e593539e864c08788`
* License: MIT (`LICENSE` confirms Copyright (c) 2024-2025 Tomislav Markovski)
* OpenAPI Specifications: `api/spec/openapi.json` and `api/spec/openapi-3.0.json`
* DBOS Transact Public Documentation: `https://docs.dbos.dev/`

## 1. Package inventory: `@dbos-argus/ui`

Inspection of `packages/ui/package.json` and its source tree under
`packages/ui/src/` confirms:

* **Package Name**: `@dbos-argus/ui`
* **Version**: `0.0.1`
* **License**: MIT
* **Module Type**: ES Module (`"type": "module"`)
* **Entry Points**: `./src/lib/index.ts` for `svelte`, `types`, and `default`
* **Declared Dependencies**:
  * `peerDependencies`: `"svelte": "^5.0.0"`
  * `devDependencies`: `"svelte": "^5.0.0"`, `"svelte-check": "^4.0.0"`, `"typescript": "^5.6.0"`, `"vitest": "^2.1.0"`
  * `dependencies`: None (zero runtime npm dependencies)

### Exported components

The package exports four Svelte components from `packages/ui/src/lib/index.ts`:

1. **`WorkflowGraph` (`WorkflowGraph.svelte`)**:
   * Props: `WorkflowGraphProps` containing `nodes: WorkflowNode[]` and `edges: WorkflowEdge[]`.
   * Implementation: A 10-line placeholder stub. The entire template consists of:
     `<div class="argus-workflow-graph" data-testid="workflow-graph"><p>WorkflowGraph stub - {nodes.length} nodes, {edges.length} edges.</p></div>`.
     It contains no graph layout logic, no SVG/canvas rendering, and no node interaction.
2. **`StatusPill` (`StatusPill.svelte`)**:
   * Props: `StatusPillProps` containing `status: WorkflowStatus` and optional `label?: string`.
   * Implementation: A 10-line span element:
     `<span class="argus-status-pill argus-status-{status}" data-testid="status-pill">{label ?? status}</span>`.
     The component contains no scoped styling and relies on undefined global CSS classes.
3. **`EventTimeline` (`EventTimeline.svelte`)**:
   * Props: `EventTimelineProps` containing `events: TimelineEvent[]`.
   * Implementation: A 16-line `<ol>` list rendering timestamp, bold label, and detail.
4. **`QueueTable` (`QueueTable.svelte`)**:
   * Props: `QueueTableProps` containing `queues: QueueRow[]`.
   * Implementation: A 27-line unstyled HTML table rendering queue name and counts (pending, running, failed).

### Framework version constraints

* **`@dbos-argus/ui`**: Pins Svelte `^5.0.0` as a peer dependency. It has no dependencies on SvelteKit or graph visualization packages.
* **`dbos-argus` application (`apps/console`)**: The actual web console application in the same monorepo pins:
  * Svelte: `^5.1.0`
  * SvelteKit: `@sveltejs/kit: ^2.8.0`, `@sveltejs/adapter-static: ^3.0.10`, `@sveltejs/vite-plugin-svelte: ^4.0.0`
  * Svelte Flow: `@xyflow/svelte: ^1.2.0`
  * Graph Layout: `elkjs: ^0.9.3`
  * UI Primitives: `bits-ui: ^2.18.0`, `shadcn-svelte: ^1.2.7`, `tailwindcss: ^4.0.0-beta.3`

Crucially, `apps/console` is private (`"private": true`) and unexported. Furthermore, `apps/console` does not import or consume `@dbos-argus/ui` in any of its routes or components. The real DAG visualization in `dbos-argus` is implemented entirely within `apps/console/src/lib/components/WorkflowFlow.svelte` (889 lines) using `@xyflow/svelte` and `elkjs`.

## 2. Component type mapping against OpenAPI schemas

Comparing the types in `packages/ui/src/lib/types.ts` against the vendored
`api/spec/openapi.json` schemas reveals significant semantic divergence.

### Workflow status mapping

`@dbos-argus/ui` defines `WorkflowStatus` as a lowercase union:
```typescript
export type WorkflowStatus =
  | "pending"
  | "running"
  | "success"
  | "error"
  | "cancelled"
  | "paused";
```

In contrast, the OpenAPI specification (`Workflow.status`) and DBOS Transact
runtime define uppercase strings:
`ENQUEUED`, `DELAYED`, `PENDING`, `SUCCESS`, `ERROR`, `MAX_RECOVERY_ATTEMPTS_EXCEEDED`, `CANCELLED`.

| OpenAPI Status | `@dbos-argus/ui` Status | Mapping Analysis |
| --- | --- | --- |
| `PENDING` | `"pending"` / `"running"` | Ambiguous. Transact marks active executions as `PENDING`. `@dbos-argus/ui` splits this into `pending` and `running`. |
| `ENQUEUED` | None | Missing. Workflows queued in partition buffers have no corresponding state in `@dbos-argus/ui`. |
| `DELAYED` | None | Missing. Workflows delayed by sleep or schedule have no representation. |
| `SUCCESS` | `"success"` | Case conversion required (`SUCCESS` -> `"success"`). |
| `ERROR` | `"error"` | Case conversion required (`ERROR` -> `"error"`). |
| `CANCELLED` | `"cancelled"` | Case conversion required (`CANCELLED` -> `"cancelled"`). |
| `MAX_RECOVERY_ATTEMPTS_EXCEEDED` | None | Missing. Workflows failing executor recovery limits have no representation. |
| None | `"paused"` | Spurious. DBOS Transact workflows do not have a paused execution state. |

`apps/console` in `dbos-argus` itself abandoned the `@dbos-argus/ui` status union,
implementing canonical uppercase `WORKFLOW_STATUSES` matching DBOS Transact
in `apps/console/src/lib/workflow-status.ts`.

### Workflow and step graph mapping

OpenAPI defines `Step` as a flat execution record:
* `stepId`: `integer` (function execution sequence index)
* `stepName`: `string`
* `output`: `string | null`
* `error`: `string | null`
* `childWorkflowId`: `string | null`
* `startedAt`: `string (date-time) | null`
* `completedAt`: `string (date-time) | null`

In contrast, `@dbos-argus/ui` expects an explicit node and edge graph:
```typescript
export interface WorkflowNode {
  id: string;
  label: string;
  status: WorkflowStatus;
}

export interface WorkflowEdge {
  id: string;
  source: string;
  target: string;
}
```

Mapping challenges:
1. **Edge Synthesis**: Conductor REST APIs do not return edges. Edges must be
   computed by sorting `stepId` values into execution sequence and nesting
   `childWorkflowId` references.
2. **Step Status**: OpenAPI `Step` has no `status` field. Step status must be
   derived by inspecting `startedAt`, `completedAt`, and `error`.
3. **No Graphical Output**: Even after constructing `nodes` and `edges`,
   `@dbos-argus/ui`'s `WorkflowGraph` renders only placeholder text.

### Queue representation mapping

`@dbos-argus/ui` defines `QueueRow`:
```typescript
export interface QueueRow {
  id: string;
  name: string;
  pending: number;
  running: number;
  failed: number;
}
```

The OpenAPI `Queue` schema represents static queue configuration:
* `name`: `string`
* `concurrency`: `integer | null`
* `workerConcurrency`: `integer | null`
* `rateLimitMax`: `integer | null`
* `rateLimitPeriodSecs`: `number | null`
* `priorityEnabled`: `boolean`
* `partitionQueue`: `boolean`
* `pollingIntervalSecs`: `number`
* `applicationName`: `string | null`

OpenAPI `Queue` objects do not contain dynamic runtime counts (`pending`,
`running`, `failed`). Populating `@dbos-argus/ui`'s `QueueTable` would require
polling and aggregating `/v2/orgs/{orgName}/apps/{appName}/queues/{queueName}/workflows`
per queue, which creates unnecessary control plane traffic.

### Events and timeline mapping

`@dbos-argus/ui` defines `TimelineEvent`:
```typescript
export interface TimelineEvent {
  id: string;
  at: string; // ISO timestamp
  label: string;
  detail?: string;
}
```

In the OpenAPI specification:
* `Event` records key-value synchronization points (`key: string`, `value: string`), without timestamps.
* Workflow timestamps live on `Workflow` (`createdAt`, `updatedAt`, `dequeuedAt`, `completedAt`).
* Step execution timestamps live on `Step` (`startedAt`, `completedAt`).
* Streaming notifications live on `Notification` and `StreamEntry`.

An adapter layer would have to synthesize an artificial `TimelineEvent` array from
multiple heterogeneous resources.

## 3. Decision: No-Go

Relay will **not** consume or depend on `@dbos-argus/ui`.

### Rationale

1. **Stub Implementation**: `@dbos-argus/ui` is an empty package scaffold. Its
   `WorkflowGraph` component does not implement graph layout or visualization.
   Consuming the package provides no functional UI capabilities.
2. **Abandoned Upstream**: The parent project `dbos-argus` does not consume
   `@dbos-argus/ui` in its own application (`apps/console`). Instead,
   `apps/console` implements its DAG directly using `@xyflow/svelte` and `elkjs`.
3. **Semantic Divergence**: The data models in `@dbos-argus/ui` diverge from
   standard DBOS Transact and Conductor v2 schemas, requiring translation layers
   that produce degraded representations.
4. **Dependency Overhead**: Adding `@dbos-argus/ui` introduces a third-party npm
   dependency with zero architectural benefit, creating version drift and supply
   chain risk.

## Outcome

The No-Go decision on `@dbos-argus/ui` held in full. The web interface shipped as
a zero-dependency vanilla JS console with no npm dependencies, avoiding
supply-chain overhead. DAG visualization shipped as the permitted bespoke SVG DAG
renderer (`WorkflowDAG.js`). UI styling and client API models are maintained directly
in `console/src/` without external framework toolchains. See [ADR 0009](../adr/0009-dashboard-web-stack.md).

## Addendum
OIDC Login flow added.
