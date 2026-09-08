// WorkflowDAG component: Interactive SVG step execution DAG for DBOS Transact workflows

export function renderWorkflowDAG(steps, onSelectStepCallbackName = "window.selectStep") {
  if (!steps || steps.length === 0) {
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

  // Sort steps by stepId
  const sorted = [...steps].sort((a, b) => (a.stepId || 0) - (b.stepId || 0));

  const nodeWidth = 200;
  const nodeHeight = 64;
  const gapX = 60;
  const startX = 30;
  const startY = 30;

  const totalWidth = startX * 2 + sorted.length * nodeWidth + (sorted.length - 1) * gapX;
  const totalHeight = startY * 2 + nodeHeight;

  let nodesSvg = "";
  let edgesSvg = "";

  for (let i = 0; i < sorted.length; i++) {
    const step = sorted[i];
    const x = startX + i * (nodeWidth + gapX);
    const y = startY;

    // Determine step status & timing
    let statusClass = "step-success";
    let statusText = "COMPLETED";
    let statusColor = "var(--color-success)";

    if (step.error) {
      statusClass = "step-error";
      statusText = "ERROR";
      statusColor = "var(--color-error)";
    } else if (!step.completedAt && step.startedAt) {
      statusClass = "step-running";
      statusText = "RUNNING";
      statusColor = "var(--color-info)";
    }

    let durationText = "";
    if (step.startedAt && step.completedAt) {
      const start = new Date(step.startedAt).getTime();
      const end = new Date(step.completedAt).getTime();
      const diffMs = Math.max(0, end - start);
      durationText = diffMs < 1000 ? `${diffMs}ms` : `${(diffMs / 1000).toFixed(2)}s`;
    }

    // Edge to next node
    if (i < sorted.length - 1) {
      const nextX = startX + (i + 1) * (nodeWidth + gapX);
      const startPointX = x + nodeWidth;
      const startPointY = y + nodeHeight / 2;
      const endPointX = nextX;
      const endPointY = startPointY;
      const midX = (startPointX + endPointX) / 2;

      edgesSvg += `
        <path d="M ${startPointX} ${startPointY} C ${midX} ${startPointY}, ${midX} ${endPointY}, ${endPointX} ${endPointY}"
              class="dag-edge" marker-end="url(#arrowhead)" />
      `;
    }

    const stepJson = encodeURIComponent(JSON.stringify(step));

    nodesSvg += `
      <g class="dag-node ${statusClass}" transform="translate(${x}, ${y})" onclick="${onSelectStepCallbackName}('${stepJson}')" cursor="pointer">
        <rect width="${nodeWidth}" height="${nodeHeight}" rx="8" class="node-bg" />
        <rect width="4" height="${nodeHeight}" rx="2" class="node-stripe" fill="${statusColor}" />

        <text x="14" y="24" class="node-step-id">#${step.stepId}</text>
        <text x="36" y="24" class="node-name" width="${nodeWidth - 45}">
          ${truncate(escapeHtml(step.stepName), 18)}
        </text>

        <text x="14" y="48" class="node-status" fill="${statusColor}">${statusText}</text>
        ${durationText ? `<text x="${nodeWidth - 12}" y="48" class="node-duration" text-anchor="end">${durationText}</text>` : ""}

        ${step.childWorkflowId ? `<rect x="${nodeWidth - 24}" y="8" width="16" height="16" rx="4" class="child-wf-badge" fill="var(--color-purple)" />
        <text x="${nodeWidth - 16}" y="20" class="child-wf-icon" text-anchor="middle" fill="#fff" font-size="10">↳</text>` : ""}
      </g>
    `;
  }

  return `
    <div class="dag-container">
      <svg class="dag-canvas" viewBox="0 0 ${Math.max(totalWidth, 600)} ${totalHeight}" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <marker id="arrowhead" markerWidth="8" markerHeight="6" refX="7" refY="3" orient="auto">
            <polygon points="0 0, 8 3, 0 6" fill="var(--color-border-strong)" />
          </marker>
        </defs>
        <g class="dag-edges">${edgesSvg}</g>
        <g class="dag-nodes">${nodesSvg}</g>
      </svg>
    </div>
  `;
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
