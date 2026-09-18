// WorkflowDAG component: Interactive SVG step execution DAG for DBOS Transact workflows

export function renderWorkflowDAG(input, onSelectStepCallbackName = "window.selectStep") {
  if (!input) {
    return renderEmptyDAG();
  }

  // Support both array of steps and hierarchical family structure
  let rootWf = { workflowName: "Workflow", workflowId: "", status: "UNKNOWN" };
  let rootSteps = [];
  let children = [];

  if (Array.isArray(input)) {
    rootSteps = [...input];
  } else if (typeof input === "object") {
    if (input.root) {
      rootWf = input.root.workflow || rootWf;
      rootSteps = Array.isArray(input.root.steps) ? [...input.root.steps] : [];
    } else if (Array.isArray(input.steps)) {
      rootSteps = [...input.steps];
    }
    if (Array.isArray(input.children)) {
      children = input.children;
    }
  }

  if (rootSteps.length === 0 && children.length === 0) {
    return renderEmptyDAG();
  }

  rootSteps.sort((a, b) => (a.stepId || 0) - (b.stepId || 0));

  const nodeWidth = 200;
  const nodeHeight = 64;
  const gapX = 54;
  const hasChildren = children.length > 0;

  const startX = hasChildren ? 44 : 32;
  const startY = hasChildren ? 74 : 36;

  let nodesSvg = "";
  let edgesSvg = "";
  let familyEdgesSvg = "";
  let cardsSvg = "";

  // 1. Root Steps layout
  let maxX = startX;
  let maxY = startY + nodeHeight;

  const parentStepCoords = new Map();

  for (let i = 0; i < rootSteps.length; i++) {
    const step = rootSteps[i];
    const x = startX + i * (nodeWidth + gapX);
    const y = startY;

    maxX = Math.max(maxX, x + nodeWidth);
    parentStepCoords.set(step.stepId, { x, y, step });
    if (step.childWorkflowId) {
      parentStepCoords.set(step.childWorkflowId, { x, y, step });
    }

    const { statusClass, statusText, statusColor } = getStepStatusMeta(step);
    const durationText = getStepDurationText(step);

    // Intra-card sequential edge
    if (i < rootSteps.length - 1) {
      const nextX = startX + (i + 1) * (nodeWidth + gapX);
      const startPointX = x + nodeWidth;
      const startPointY = y + nodeHeight / 2;
      const endPointX = nextX;
      const endPointY = startPointY;
      const midX = (startPointX + endPointX) / 2;

      edgesSvg += `
        <path d="M ${startPointX} ${startPointY} C ${midX} ${startPointY}, ${midX} ${endPointY}, ${endPointX} ${endPointY}"
              class="dag-edge" marker-end="url(#arrowhead-default)" />
      `;
    }

    nodesSvg += renderStepNodeSvg(step, x, y, nodeWidth, nodeHeight, statusClass, statusText, statusColor, durationText);
  }

  // 2. Root workflow container card if family mode
  if (hasChildren) {
    const rootCardWidth = Math.max(maxX + startX - 20, 560);
    const rootCardHeight = nodeHeight + 80;
    const rootStatusColor = getWorkflowStatusColor(rootWf.status);

    cardsSvg += `
      <g class="dag-group-card root-group" transform="translate(16, 20)">
        <rect width="${rootCardWidth}" height="${rootCardHeight}" rx="10" class="group-card-bg" />
        <rect width="${rootCardWidth}" height="36" rx="10" class="group-card-header-bg" />
        <rect y="26" width="${rootCardWidth}" height="10" class="group-card-header-fill" />
        <circle cx="16" cy="18" r="4.5" fill="${rootStatusColor}" />
        <text x="28" y="22" class="group-card-title">Root Workflow: ${escapeHtml(rootWf.workflowName || rootWf.workflowId || "unnamed")}</text>
        <text x="${rootCardWidth - 16}" y="22" class="group-card-meta" text-anchor="end">${rootSteps.length} steps</text>
      </g>
    `;
    maxY = Math.max(maxY, 20 + rootCardHeight);
  }

  // 3. Child workflow cards & intra-child steps layout
  if (hasChildren) {
    let currentChildX = 24;
    const childY = maxY + 60;

    for (let c = 0; c < children.length; c++) {
      const child = children[c];
      const childWf = child.workflow || {};
      const childSteps = Array.isArray(child.steps) ? [...child.steps] : [];
      childSteps.sort((a, b) => (a.stepId || 0) - (b.stepId || 0));

      const childStepCount = childSteps.length;
      const childCardWidth = Math.max(280, 48 + Math.max(1, childStepCount) * (nodeWidth + gapX) - gapX);
      const childCardHeight = childStepCount > 0 ? (nodeHeight + 84) : 96;

      // Find parent step coordinate to align near parent or link cleanly
      let targetX = currentChildX;
      const parentCoord = parentStepCoords.get(child.stepId) || parentStepCoords.get(child.childWorkflowId);
      if (parentCoord) {
        targetX = Math.max(currentChildX, parentCoord.x - 20);
      }

      const childCardX = targetX;
      currentChildX = childCardX + childCardWidth + 40;
      maxX = Math.max(maxX, childCardX + childCardWidth);
      maxY = Math.max(maxY, childY + childCardHeight);

      const childStatusColor = getWorkflowStatusColor(childWf.status);

      // Child Workflow Card
      cardsSvg += `
        <g class="dag-group-card child-group" transform="translate(${childCardX}, ${childY})">
          <rect width="${childCardWidth}" height="${childCardHeight}" rx="10" class="group-card-bg child-card-border" />
          <rect width="${childCardWidth}" height="36" rx="10" class="group-card-header-bg" />
          <rect y="26" width="${childCardWidth}" height="10" class="group-card-header-fill" />
          <circle cx="16" cy="18" r="4.5" fill="${childStatusColor}" />
          <text x="28" y="22" class="group-card-title">Child Workflow: ${escapeHtml(childWf.workflowName || child.childWorkflowId)}</text>
          <g class="child-card-action" data-action="viewChildWorkflow" data-child-wf-id="${escapeHtml(child.childWorkflowId)}" cursor="pointer" role="button" tabindex="0" aria-label="Inspect child workflow">
            <rect x="${childCardWidth - 84}" y="7" width="72" height="22" rx="4" class="action-pill-bg" />
            <text x="${childCardWidth - 48}" y="21" class="action-pill-text" text-anchor="middle">Inspect</text>
          </g>
        </g>
      `;

      // Family connection edge from parent step to child workflow card
      if (parentCoord) {
        const fromX = parentCoord.x + nodeWidth / 2;
        const fromY = parentCoord.y + nodeHeight;
        const toX = childCardX + 40;
        const toY = childY;
        const midY = (fromY + toY) / 2;

        familyEdgesSvg += `
          <path d="M ${fromX} ${fromY} C ${fromX} ${midY}, ${toX} ${midY}, ${toX} ${toY}"
                class="dag-family-edge ${getOutcomeClass(childWf.status)}"
                marker-end="url(#arrowhead-${getMarkerSuffix(childWf.status)})" />
        `;
      }

      // Steps within child card
      if (childStepCount === 0) {
        cardsSvg += `
          <text x="${childCardX + 24}" y="${childY + 65}" class="child-empty-text">
            No step executions recorded for child workflow
          </text>
        `;
      } else {
        for (let j = 0; j < childSteps.length; j++) {
          const cStep = childSteps[j];
          const csX = childCardX + 24 + j * (nodeWidth + gapX);
          const csY = childY + 54;

          const meta = getStepStatusMeta(cStep);
          const durText = getStepDurationText(cStep);

          if (j < childSteps.length - 1) {
            const nextCsX = childCardX + 24 + (j + 1) * (nodeWidth + gapX);
            const sX = csX + nodeWidth;
            const sY = csY + nodeHeight / 2;
            const eX = nextCsX;
            const eY = sY;
            const mX = (sX + eX) / 2;

            edgesSvg += `
              <path d="M ${sX} ${sY} C ${mX} ${sY}, ${mX} ${eY}, ${eX} ${eY}"
                    class="dag-edge" marker-end="url(#arrowhead-default)" />
            `;
          }

          nodesSvg += renderStepNodeSvg(cStep, csX, csY, nodeWidth, nodeHeight, meta.statusClass, meta.statusText, meta.statusColor, durText);
        }
      }
    }
  }

  const totalWidth = Math.max(maxX + 60, 780);
  const totalHeight = Math.max(maxY + 60, 180);

  return `
    <div class="dag-container" role="region" aria-label="Workflow Execution Steps DAG" id="dag-container">
      <div class="dag-toolbar" role="toolbar" aria-label="DAG Viewport Controls">
        <div class="dag-legend">
          <span class="legend-item"><span class="legend-dot status-success"></span> Completed</span>
          <span class="legend-item"><span class="legend-dot status-running"></span> Running</span>
          <span class="legend-item"><span class="legend-dot status-error"></span> Error</span>
          ${hasChildren ? `<span class="legend-item"><span class="legend-dot status-child"></span> Child WF</span>` : ""}
        </div>
        <div class="dag-controls">
          <button class="dag-btn" data-action="dagZoomIn" title="Zoom in" aria-label="Zoom in">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="11" y1="8" x2="11" y2="14"/><line x1="8" y1="11" x2="14" y2="11"/></svg>
          </button>
          <button class="dag-btn" data-action="dagZoomOut" title="Zoom out" aria-label="Zoom out">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/><line x1="8" y1="11" x2="14" y2="11"/></svg>
          </button>
          <button class="dag-btn" data-action="dagReset" title="Reset view" aria-label="Reset view">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><polyline points="3 3 3 8 8 8"/></svg>
            <span style="margin-left:4px; font-size:11px;">Reset</span>
          </button>
        </div>
      </div>
      <div class="dag-viewport" id="dag-viewport" tabindex="0" role="region" aria-label="Workflow DAG Canvas. Use mouse or buttons to zoom and pan.">
        <svg class="dag-canvas" id="dag-canvas" viewBox="0 0 ${totalWidth} ${totalHeight}" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <pattern id="dag-dot-grid" width="22" height="22" patternUnits="userSpaceOnUse">
              <circle cx="2" cy="2" r="1.3" fill="var(--border-strong)" opacity="0.45" />
            </pattern>
            <marker id="arrowhead-default" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
              <polygon points="0 0, 8 3, 0 6" fill="var(--border-strong)" />
            </marker>
            <marker id="arrowhead-success" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
              <polygon points="0 0, 8 3, 0 6" fill="var(--color-success)" />
            </marker>
            <marker id="arrowhead-error" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
              <polygon points="0 0, 8 3, 0 6" fill="var(--color-error)" />
            </marker>
            <marker id="arrowhead-running" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
              <polygon points="0 0, 8 3, 0 6" fill="var(--color-info)" />
            </marker>
          </defs>
          <rect width="100%" height="100%" fill="url(#dag-dot-grid)" class="canvas-grid-bg" />
          <g class="dag-zoom-layer" id="dag-zoom-layer">
            <g class="dag-cards">${cardsSvg}</g>
            <g class="dag-family-edges">${familyEdgesSvg}</g>
            <g class="dag-edges">${edgesSvg}</g>
            <g class="dag-nodes">${nodesSvg}</g>
          </g>
        </svg>
      </div>
    </div>
  `;
}

function renderStepNodeSvg(step, x, y, width, height, statusClass, statusText, statusColor, durationText) {
  const isChildCaller = Boolean(step.childWorkflowId);
  return `
    <g class="dag-node ${statusClass}" transform="translate(${x}, ${y})"
       data-step-json="${escapeHtml(JSON.stringify(step))}"
       cursor="pointer" tabindex="0" role="button"
       aria-label="Step ${step.stepId}: ${escapeHtml(step.stepName || "step")} (${statusText})">
      <rect width="${width}" height="${height}" rx="8" class="node-bg" />
      <circle cx="18" cy="20" r="4.5" fill="${statusColor}" />

      <text x="28" y="24" class="node-step-id">#${step.stepId}</text>
      <text x="48" y="24" class="node-name" width="${width - 55}">
        ${truncate(escapeHtml(step.stepName || "step"), 15)}
      </text>

      <text x="18" y="48" class="node-status" fill="${statusColor}">${statusText}</text>
      ${durationText ? `<text x="${width - 12}" y="48" class="node-duration" text-anchor="end">${durationText}</text>` : ""}

      ${isChildCaller ? `
        <rect x="${width - 24}" y="8" width="16" height="16" rx="4" class="child-wf-badge" fill="var(--color-purple)" />
        <path d="M ${width - 18} 12 L ${width - 14} 16 L ${width - 10} 12" stroke="#ffffff" stroke-width="1.8" fill="none" />
      ` : ""}
    </g>
  `;
}

function renderEmptyDAG() {
  return `
    <div class="dag-empty">
      <svg class="dag-empty-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
        <rect x="3" y="3" width="18" height="18" rx="2" stroke-dasharray="4 4" />
        <circle cx="8.5" cy="8.5" r="1.5" />
        <polyline points="21 15 16 10 5 21" />
      </svg>
      <p>No step executions recorded for this workflow.</p>
    </div>
  `;
}

function getStepStatusMeta(step) {
  if (step.error) {
    return { statusClass: "step-error", statusText: "ERROR", statusColor: "var(--color-error)" };
  }
  if (!step.completedAt && step.startedAt) {
    return { statusClass: "step-running", statusText: "RUNNING", statusColor: "var(--color-info)" };
  }
  if (step.completedAt) {
    return { statusClass: "step-success", statusText: "COMPLETED", statusColor: "var(--color-success)" };
  }
  return { statusClass: "step-pending", statusText: "PENDING", statusColor: "var(--text-tertiary)" };
}

function getStepDurationText(step) {
  if (step.startedAt && step.completedAt) {
    const start = new Date(step.startedAt).getTime();
    const end = new Date(step.completedAt).getTime();
    const diffMs = Math.max(0, end - start);
    return diffMs < 1000 ? `${diffMs}ms` : `${(diffMs / 1000).toFixed(2)}s`;
  }
  return "";
}

function getWorkflowStatusColor(status) {
  switch (String(status || "").toUpperCase()) {
    case "SUCCESS":
      return "var(--color-success)";
    case "ERROR":
      return "var(--color-error)";
    case "PENDING":
    case "ENQUEUED":
      return "var(--color-info)";
    default:
      return "var(--text-tertiary)";
  }
}

function getOutcomeClass(status) {
  switch (String(status || "").toUpperCase()) {
    case "SUCCESS": return "edge-success";
    case "ERROR": return "edge-error";
    case "PENDING":
    case "ENQUEUED": return "edge-running";
    default: return "edge-default";
  }
}

function getMarkerSuffix(status) {
  switch (String(status || "").toUpperCase()) {
    case "SUCCESS": return "success";
    case "ERROR": return "error";
    case "PENDING":
    case "ENQUEUED": return "running";
    default: return "default";
  }
}

function truncate(str, maxLen) {
  if (!str) return "";
  return str.length > maxLen ? str.substring(0, maxLen - 1) + "…" : str;
}

function escapeHtml(str) {
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}
