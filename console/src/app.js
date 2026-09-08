// Relay Dashboard SPA Application

import { ApiClient } from "./lib/api/client.js";
import { renderStatusPill } from "./lib/components/StatusPill.js";
import { renderJsonViewer } from "./lib/components/JsonViewer.js";
import { renderWorkflowDAG } from "./lib/components/WorkflowDAG.js";

class DashboardApp {
  constructor() {
    this.client = new ApiClient();
    this.currentRoute = "fleet";
    this.orgName = "default";
    this.appName = "";
    this.apps = [];
    this.selectedWorkflowId = null;
    this.selectedStep = null;
    this.theme = localStorage.getItem("relay-theme") || (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");

    // Expose global callback for SVG DAG node clicks
    window.selectStep = (encodedStepJson) => {
      try {
        const step = JSON.parse(decodeURIComponent(encodedStepJson));
        this.selectedStep = step;
        this.renderStepModal(step);
      } catch (err) {
        console.error("Failed to parse step:", err);
      }
    };
  }

  async init() {
    this.applyTheme(this.theme);
    window.addEventListener("hashchange", () => this.handleRouting());

    await this.loadApplications();
    this.handleRouting();
  }

  applyTheme(theme) {
    this.theme = theme;
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("relay-theme", theme);
  }

  toggleTheme() {
    this.applyTheme(this.theme === "dark" ? "light" : "dark");
  }

  async loadApplications() {
    try {
      this.apps = await this.client.listApplications(this.orgName);
      if (this.apps && this.apps.length > 0 && !this.appName) {
        this.appName = this.apps[0].name;
      }
    } catch (err) {
      console.warn("Listing applications:", err);
      this.apps = [];
    }
  }

  handleRouting() {
    const hash = window.location.hash.replace(/^#\/?/, "") || "fleet";
    const parts = hash.split("/");
    const route = parts[0];

    if (route === "workflow" && parts[1]) {
      this.currentRoute = "workflow-detail";
      this.selectedWorkflowId = parts[1];
    } else {
      this.currentRoute = route;
    }

    this.render();
  }

  navigate(route) {
    window.location.hash = "#/" + route;
  }

  render() {
    const appEl = document.getElementById("app");
    if (!appEl) return;

    appEl.innerHTML = `
      ${this.renderSidebar()}
      <div class="main-wrapper">
        ${this.renderTopHeader()}
        <main class="content-area" id="content-view">
          <div class="loading-spinner">Loading...</div>
        </main>
      </div>
      <div id="modal-root"></div>
    `;

    this.renderContentView();
  }

  renderSidebar() {
    const navItems = [
      { id: "fleet", label: "Fleet & Apps", icon: `<path d="M4 6h16M4 12h16M4 18h16"/>` },
      { id: "workflows", label: "Workflows", icon: `<path d="M22 12h-4l-3 9L9 3l-3 9H2"/>` },
      { id: "queues", label: "Queues", icon: `<rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/>` },
      { id: "schedules", label: "Schedules", icon: `<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>` },
      { id: "alerting", label: "Alert Rules", icon: `<path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/>` },
      { id: "keys", label: "API Keys", icon: `<path d="M21 2l-2 2m-1.5 1.5L10 13l-4 4-2-2-4 4 3 3 7-7 7.5-7.5z"/>` },
    ];

    return `
      <aside class="sidebar">
        <div class="sidebar-header">
          <a href="#/fleet" class="brand-logo">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" fill="var(--color-primary)" stroke="none"/>
            </svg>
            <span>Relay</span>
          </a>
          <span class="brand-badge">Dashboard</span>
        </div>
        <nav class="sidebar-nav">
          ${navItems.map(item => `
            <a class="nav-item ${this.currentRoute === item.id || (this.currentRoute === 'workflow-detail' && item.id === 'workflows') ? 'active' : ''}"
               onclick="window.app.navigate('${item.id}')">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                ${item.icon}
              </svg>
              <span>${item.label}</span>
            </a>
          `).join("")}
        </nav>
        <div class="sidebar-footer">
          <span>Relay Control Plane</span>
          <button class="btn btn-xs btn-secondary" onclick="window.app.toggleTheme()">
            ${this.theme === "dark" ? "☀️ Light" : "🌙 Dark"}
          </button>
        </div>
      </aside>
    `;
  }

  renderTopHeader() {
    return `
      <header class="top-header">
        <div class="header-left">
          <h1 class="header-title">${this.getRouteTitle()}</h1>
        </div>
        <div class="header-right">
          <div class="selector-group">
            <label class="form-label" style="margin:0;">App:</label>
            <select class="select-sm" onchange="window.app.onAppChange(this.value)">
              ${this.apps.map(a => `<option value="${escapeHtml(a.name)}" ${a.name === this.appName ? "selected" : ""}>${escapeHtml(a.name)}</option>`).join("")}
              ${this.apps.length === 0 ? `<option value="">No applications</option>` : ""}
            </select>
          </div>
          <button class="btn btn-xs btn-secondary" onclick="window.app.renderContentView()">↻ Refresh</button>
        </div>
      </header>
    `;
  }

  getRouteTitle() {
    switch (this.currentRoute) {
      case "fleet": return "Fleet & Applications";
      case "workflows": return "Workflows";
      case "workflow-detail": return `Workflow: ${this.selectedWorkflowId || ""}`;
      case "queues": return "Queues";
      case "schedules": return "Schedules";
      case "alerting": return "Alerting Rules";
      case "keys": return "API Keys";
      default: return "Relay Dashboard";
    }
  }

  onAppChange(newAppName) {
    this.appName = newAppName;
    this.renderContentView();
  }

  async renderContentView() {
    const el = document.getElementById("content-view");
    if (!el) return;

    switch (this.currentRoute) {
      case "fleet":
        await this.renderFleetScreen(el);
        break;
      case "workflows":
        await this.renderWorkflowsScreen(el);
        break;
      case "workflow-detail":
        await this.renderWorkflowDetailScreen(el);
        break;
      case "queues":
        await this.renderQueuesScreen(el);
        break;
      case "schedules":
        await this.renderSchedulesScreen(el);
        break;
      case "alerting":
        await this.renderAlertingScreen(el);
        break;
      case "keys":
        await this.renderKeysScreen(el);
        break;
      default:
        el.innerHTML = `<div class="card"><div class="card-body">Select a view from the sidebar.</div></div>`;
    }
  }

  // --- SCREEN 1: FLEET & APPLICATIONS ---
  async renderFleetScreen(el) {
    el.innerHTML = `<div class="loading-spinner">Loading fleet information...</div>`;

    try {
      await this.loadApplications();
      let executors = [];
      if (this.appName) {
        executors = await this.client.listExecutors(this.orgName, this.appName);
      }

      const activeExecutors = executors.filter(e => e.status === "HEALTHY").length;
      const disconnectedExecutors = executors.filter(e => e.status === "DISCONNECTED").length;

      el.innerHTML = `
        <div class="stat-grid">
          <div class="stat-card">
            <span class="stat-label">Applications</span>
            <span class="stat-value">${this.apps.length}</span>
          </div>
          <div class="stat-card">
            <span class="stat-label">Connected Executors</span>
            <span class="stat-value" style="color: var(--color-success);">${activeExecutors}</span>
          </div>
          <div class="stat-card">
            <span class="stat-label">Disconnected Executors</span>
            <span class="stat-value" style="color: var(--color-warning);">${disconnectedExecutors}</span>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <span class="card-title">Connected Executors (${this.appName || 'No app'})</span>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Executor ID</th>
                  <th>Status</th>
                  <th>Hostname</th>
                  <th>Version</th>
                  <th>Language</th>
                  <th>Last Seen</th>
                </tr>
              </thead>
              <tbody>
                ${executors.length > 0 ? executors.map(e => `
                  <tr>
                    <td><code>${escapeHtml(e.executorId)}</code></td>
                    <td>${renderStatusPill(e.status)}</td>
                    <td>${escapeHtml(e.hostname || "localhost")}</td>
                    <td><span class="badge">${escapeHtml(e.appVersion || "v1.0.0")}</span></td>
                    <td>${escapeHtml(e.language || "unknown")}</td>
                    <td>${formatTimestamp(e.updatedAt)}</td>
                  </tr>
                `).join("") : `<tr><td colspan="6" style="text-align:center; color:var(--text-tertiary);">No executors registered for this application.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <span class="card-title">Registered Applications</span>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Status</th>
                  <th>Timeout</th>
                  <th>Private Mode</th>
                </tr>
              </thead>
              <tbody>
                ${this.apps.map(a => `
                  <tr class="clickable" onclick="window.app.onAppChange('${escapeHtml(a.name)}')">
                    <td><strong>${escapeHtml(a.name)}</strong></td>
                    <td>${renderStatusPill(a.status)}</td>
                    <td>${a.executorTimeoutSecs || 60}s</td>
                    <td>${a.privateMode ? "Enabled" : "Disabled"}</td>
                  </tr>
                `).join("")}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error);">Failed to load fleet: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  // --- SCREEN 2: WORKFLOW SEARCH & LIST ---
  async renderWorkflowsScreen(el) {
    el.innerHTML = `<div class="loading-spinner">Loading workflows...</div>`;

    try {
      const workflows = await this.client.listWorkflows(this.orgName, this.appName, { limit: 50 });

      el.innerHTML = `
        <div class="toolbar">
          <div class="filter-group">
            <input type="text" id="filter-id" class="input-text" placeholder="Search workflow ID..." oninput="window.app.filterWorkflowsTable(this.value)">
            <select id="filter-status" class="select-sm" onchange="window.app.filterWorkflowsStatus(this.value)">
              <option value="">All Statuses</option>
              <option value="SUCCESS">SUCCESS</option>
              <option value="PENDING">PENDING</option>
              <option value="ERROR">ERROR</option>
              <option value="CANCELLED">CANCELLED</option>
              <option value="ENQUEUED">ENQUEUED</option>
            </select>
          </div>
          <div>
            <span class="text-secondary" style="font-size:12px;">Showing ${workflows.length} workflows</span>
          </div>
        </div>

        <div class="card">
          <div class="table-container">
            <table class="data-table" id="workflows-table">
              <thead>
                <tr>
                  <th>Workflow ID</th>
                  <th>Status</th>
                  <th>Workflow Name</th>
                  <th>Queue</th>
                  <th>Version</th>
                  <th>Created At</th>
                  <th>Duration</th>
                </tr>
              </thead>
              <tbody>
                ${workflows.length > 0 ? workflows.map(wf => `
                  <tr class="clickable" onclick="window.app.navigate('workflow/${encodeURIComponent(wf.workflowId)}')">
                    <td><code>${escapeHtml(wf.workflowId)}</code></td>
                    <td>${renderStatusPill(wf.status)}</td>
                    <td><strong>${escapeHtml(wf.workflowName || "unnamed")}</strong></td>
                    <td>${escapeHtml(wf.queueName || "default")}</td>
                    <td>${escapeHtml(wf.appVersion || "-")}</td>
                    <td>${formatTimestamp(wf.createdAt)}</td>
                    <td>${calculateDuration(wf.createdAt, wf.completedAt)}</td>
                  </tr>
                `).join("") : `<tr><td colspan="7" style="text-align:center; color:var(--text-tertiary);">No workflows found.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error);">Failed to load workflows: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  filterWorkflowsTable(query) {
    const q = query.toLowerCase();
    const rows = document.querySelectorAll("#workflows-table tbody tr");
    rows.forEach(r => {
      const text = r.innerText.toLowerCase();
      r.style.display = text.includes(q) ? "" : "none";
    });
  }

  filterWorkflowsStatus(status) {
    const rows = document.querySelectorAll("#workflows-table tbody tr");
    rows.forEach(r => {
      if (!status) {
        r.style.display = "";
      } else {
        const text = r.innerText.toUpperCase();
        r.style.display = text.includes(status) ? "" : "none";
      }
    });
  }

  // --- SCREEN 3: WORKFLOW DETAIL & STEP DAG ---
  async renderWorkflowDetailScreen(el) {
    el.innerHTML = `<div class="loading-spinner">Loading workflow details...</div>`;

    try {
      const wf = await this.client.getWorkflow(this.orgName, this.appName, this.selectedWorkflowId);
      const steps = await this.client.listSteps(this.orgName, this.appName, this.selectedWorkflowId);

      el.innerHTML = `
        <div style="margin-bottom: 16px;">
          <a class="btn btn-xs btn-secondary" onclick="window.app.navigate('workflows')">← Back to workflows</a>
        </div>

        <div class="card">
          <div class="card-header">
            <div style="display:flex; align-items:center; gap:12px;">
              <span class="card-title">Workflow: <code>${escapeHtml(wf.workflowId)}</code></span>
              ${renderStatusPill(wf.status)}
            </div>
            <div style="display:flex; gap:8px;">
              ${wf.status === "PENDING" || wf.status === "ENQUEUED" ? `
                <button class="btn btn-sm btn-danger" onclick="window.app.cancelWorkflow('${escapeHtml(wf.workflowId)}')">Cancel</button>
              ` : ""}
              ${wf.status === "CANCELLED" ? `
                <button class="btn btn-sm btn-primary" onclick="window.app.resumeWorkflow('${escapeHtml(wf.workflowId)}')">Resume</button>
              ` : ""}
              ${wf.status === "ERROR" ? `
                <button class="btn btn-sm btn-primary" onclick="window.app.restartWorkflow('${escapeHtml(wf.workflowId)}')">Restart</button>
              ` : ""}
            </div>
          </div>
          <div class="card-body">
            <div class="stat-grid" style="grid-template-columns: repeat(4, 1fr); margin-bottom: 0;">
              <div>
                <span class="stat-label">Workflow Name</span>
                <div><strong>${escapeHtml(wf.workflowName || "unnamed")}</strong></div>
              </div>
              <div>
                <span class="stat-label">Queue</span>
                <div>${escapeHtml(wf.queueName || "default")}</div>
              </div>
              <div>
                <span class="stat-label">Created At</span>
                <div>${formatTimestamp(wf.createdAt)}</div>
              </div>
              <div>
                <span class="stat-label">Duration</span>
                <div>${calculateDuration(wf.createdAt, wf.completedAt)}</div>
              </div>
            </div>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <span class="card-title">Execution Steps (${steps.length})</span>
            <span style="font-size:12px; color:var(--text-secondary);">Click any step to view parameters & output</span>
          </div>
          <div class="card-body" style="padding:0;">
            ${renderWorkflowDAG(steps, "window.selectStep")}
          </div>
        </div>

        <div class="card">
          <div class="tab-list">
            <button class="tab-btn active" id="tab-btn-io" onclick="window.app.switchWfTab('io')">Inputs & Outputs</button>
            <button class="tab-btn" id="tab-btn-events" onclick="window.app.switchWfTab('events')">Events</button>
            <button class="tab-btn" id="tab-btn-notifications" onclick="window.app.switchWfTab('notifications')">Notifications</button>
          </div>
          <div class="card-body" id="wf-tab-content">
            <div style="display:grid; grid-template-columns: 1fr 1fr; gap: 16px;">
              <div>
                <h4 style="margin-bottom:8px; font-size:13px;">Workflow Input</h4>
                ${renderJsonViewer(wf.input, "Input")}
              </div>
              <div>
                <h4 style="margin-bottom:8px; font-size:13px;">Workflow Output / Error</h4>
                ${wf.error ? renderJsonViewer(wf.error, "Error") : renderJsonViewer(wf.output, "Output")}
              </div>
            </div>
          </div>
        </div>
      `;
    } catch (err) {
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error);">Failed to load workflow: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  async switchWfTab(tab) {
    document.querySelectorAll(".tab-btn").forEach(b => b.classList.remove("active"));
    const btn = document.getElementById(`tab-btn-${tab}`);
    if (btn) btn.classList.add("active");

    const content = document.getElementById("wf-tab-content");
    if (!content) return;

    if (tab === "io") {
      const wf = await this.client.getWorkflow(this.orgName, this.appName, this.selectedWorkflowId);
      content.innerHTML = `
        <div style="display:grid; grid-template-columns: 1fr 1fr; gap: 16px;">
          <div>
            <h4 style="margin-bottom:8px; font-size:13px;">Workflow Input</h4>
            ${renderJsonViewer(wf.input, "Input")}
          </div>
          <div>
            <h4 style="margin-bottom:8px; font-size:13px;">Workflow Output / Error</h4>
            ${wf.error ? renderJsonViewer(wf.error, "Error") : renderJsonViewer(wf.output, "Output")}
          </div>
        </div>
      `;
    } else if (tab === "events") {
      content.innerHTML = `<div class="loading-spinner">Loading events...</div>`;
      const events = await this.client.getWorkflowEvents(this.orgName, this.appName, this.selectedWorkflowId);
      content.innerHTML = `
        <table class="data-table">
          <thead><tr><th>Event Key</th><th>Value</th></tr></thead>
          <tbody>
            ${events.length > 0 ? events.map(e => `
              <tr><td><code>${escapeHtml(e.key)}</code></td><td>${renderJsonViewer(e.value)}</td></tr>
            `).join("") : `<tr><td colspan="2" style="text-align:center; color:var(--text-tertiary);">No events recorded.</td></tr>`}
          </tbody>
        </table>
      `;
    } else if (tab === "notifications") {
      content.innerHTML = `<div class="loading-spinner">Loading notifications...</div>`;
      const notifs = await this.client.getWorkflowNotifications(this.orgName, this.appName, this.selectedWorkflowId);
      content.innerHTML = `
        <table class="data-table">
          <thead><tr><th>Topic</th><th>Message</th><th>Consumed</th><th>Time</th></tr></thead>
          <tbody>
            ${notifs.length > 0 ? notifs.map(n => `
              <tr>
                <td><code>${escapeHtml(n.topic || "-")}</code></td>
                <td>${escapeHtml(n.message)}</td>
                <td>${n.consumed ? "Yes" : "No"}</td>
                <td>${formatTimestamp(n.createdAt)}</td>
              </tr>
            `).join("") : `<tr><td colspan="4" style="text-align:center; color:var(--text-tertiary);">No notifications recorded.</td></tr>`}
          </tbody>
        </table>
      `;
    }
  }

  async cancelWorkflow(id) {
    if (!confirm("Are you sure you want to cancel this workflow?")) return;
    try {
      await this.client.cancelWorkflow(this.orgName, this.appName, id);
      this.renderWorkflowDetailScreen(document.getElementById("content-view"));
    } catch (err) {
      alert("Failed to cancel: " + err.message);
    }
  }

  async resumeWorkflow(id) {
    try {
      await this.client.resumeWorkflow(this.orgName, this.appName, id);
      this.renderWorkflowDetailScreen(document.getElementById("content-view"));
    } catch (err) {
      alert("Failed to resume: " + err.message);
    }
  }

  async restartWorkflow(id) {
    try {
      const res = await this.client.restartWorkflow(this.orgName, this.appName, id);
      alert(`Restarted workflow! New ID: ${res.workflowId}`);
      this.navigate(`workflow/${encodeURIComponent(res.workflowId)}`);
    } catch (err) {
      alert("Failed to restart: " + err.message);
    }
  }

  renderStepModal(step) {
    const root = document.getElementById("modal-root");
    if (!root) return;

    root.innerHTML = `
      <div class="modal-overlay" onclick="if(event.target === this) window.app.closeModal()">
        <div class="modal-dialog">
          <div class="modal-header">
            <span>Step #${step.stepId}: ${escapeHtml(step.stepName)}</span>
            <button class="btn btn-xs btn-secondary" onclick="window.app.closeModal()">✕</button>
          </div>
          <div class="modal-body">
            <div style="display:flex; justify-content:space-between; align-items:center;">
              <span class="stat-label">Status</span>
              ${renderStatusPill(step.error ? "ERROR" : step.completedAt ? "SUCCESS" : "PENDING")}
            </div>
            <div>
              <span class="stat-label">Execution Time</span>
              <div style="font-size:12px; margin-top:2px;">
                Started: ${formatTimestamp(step.startedAt)}<br>
                Completed: ${formatTimestamp(step.completedAt)}
              </div>
            </div>
            ${step.childWorkflowId ? `
              <div>
                <span class="stat-label">Child Workflow</span>
                <div>
                  <a class="btn btn-xs btn-primary" onclick="window.app.closeModal(); window.app.navigate('workflow/${encodeURIComponent(step.childWorkflowId)}')">
                    View Child Workflow: ${escapeHtml(step.childWorkflowId)}
                  </a>
                </div>
              </div>
            ` : ""}
            <div>
              <span class="stat-label">Output / Result</span>
              ${renderJsonViewer(step.output, "Step Output")}
            </div>
            ${step.error ? `
              <div>
                <span class="stat-label" style="color:var(--color-error);">Error Stack</span>
                ${renderJsonViewer(step.error, "Error Details")}
              </div>
            ` : ""}
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-secondary" onclick="window.app.closeModal()">Close</button>
          </div>
        </div>
      </div>
    `;
  }

  closeModal() {
    const root = document.getElementById("modal-root");
    if (root) root.innerHTML = "";
  }

  // --- SCREEN 4: QUEUES ---
  async renderQueuesScreen(el) {
    el.innerHTML = `<div class="loading-spinner">Loading queues...</div>`;

    try {
      const queues = await this.client.listQueues(this.orgName, this.appName);

      el.innerHTML = `
        <div class="card">
          <div class="card-header">
            <span class="card-title">Queues (${this.appName || 'No app'})</span>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Queue Name</th>
                  <th>Concurrency</th>
                  <th>Worker Concurrency</th>
                  <th>Rate Limit</th>
                  <th>Priority</th>
                  <th>Partitioned</th>
                </tr>
              </thead>
              <tbody>
                ${queues.length > 0 ? queues.map(q => `
                  <tr>
                    <td><strong>${escapeHtml(q.name)}</strong></td>
                    <td>${q.concurrency !== null ? q.concurrency : "Unlimited"}</td>
                    <td>${q.workerConcurrency !== null ? q.workerConcurrency : "-"}</td>
                    <td>${q.rateLimitMax ? `${q.rateLimitMax} / ${q.rateLimitPeriodSecs}s` : "None"}</td>
                    <td>${q.priorityEnabled ? "Yes" : "No"}</td>
                    <td>${q.partitionQueue ? "Yes" : "No"}</td>
                  </tr>
                `).join("") : `<tr><td colspan="6" style="text-align:center; color:var(--text-tertiary);">No queues configured.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error);">Failed to load queues: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  // --- SCREEN 5: SCHEDULES ---
  async renderSchedulesScreen(el) {
    el.innerHTML = `<div class="loading-spinner">Loading schedules...</div>`;

    try {
      const schedules = await this.client.listSchedules(this.orgName, this.appName);

      el.innerHTML = `
        <div class="card">
          <div class="card-header">
            <span class="card-title">Scheduled Jobs (${this.appName || 'No app'})</span>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Schedule Name</th>
                  <th>Workflow Name</th>
                  <th>Cron Expression</th>
                  <th>Status</th>
                  <th>Last Fired</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                ${schedules.length > 0 ? schedules.map(s => `
                  <tr>
                    <td><strong>${escapeHtml(s.scheduleName)}</strong></td>
                    <td>${escapeHtml(s.workflowName)}</td>
                    <td><code>${escapeHtml(s.cronExpression)}</code></td>
                    <td>${renderStatusPill(s.status)}</td>
                    <td>${formatTimestamp(s.lastFiredAt)}</td>
                    <td>
                      <div style="display:flex; gap:4px;">
                        ${s.status === "ACTIVE" ? `
                          <button class="btn btn-xs btn-secondary" onclick="window.app.pauseSchedule('${escapeHtml(s.scheduleName)}')">Pause</button>
                        ` : `
                          <button class="btn btn-xs btn-secondary" onclick="window.app.resumeSchedule('${escapeHtml(s.scheduleName)}')">Resume</button>
                        `}
                        <button class="btn btn-xs btn-primary" onclick="window.app.triggerSchedule('${escapeHtml(s.scheduleName)}')">Trigger Now</button>
                      </div>
                    </td>
                  </tr>
                `).join("") : `<tr><td colspan="6" style="text-align:center; color:var(--text-tertiary);">No schedules configured.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error);">Failed to load schedules: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  async pauseSchedule(name) {
    try {
      await this.client.pauseSchedule(this.orgName, this.appName, name);
      this.renderContentView();
    } catch (err) {
      alert("Failed to pause schedule: " + err.message);
    }
  }

  async resumeSchedule(name) {
    try {
      await this.client.resumeSchedule(this.orgName, this.appName, name);
      this.renderContentView();
    } catch (err) {
      alert("Failed to resume schedule: " + err.message);
    }
  }

  async triggerSchedule(name) {
    try {
      const res = await this.client.triggerSchedule(this.orgName, this.appName, name);
      alert(`Triggered schedule! Workflow ID: ${res.workflowId}`);
      this.navigate(`workflow/${encodeURIComponent(res.workflowId)}`);
    } catch (err) {
      alert("Failed to trigger schedule: " + err.message);
    }
  }

  // --- SCREEN 6: ALERTING RULES ---
  async renderAlertingScreen(el) {
    el.innerHTML = `<div class="loading-spinner">Loading alerting rules...</div>`;

    try {
      const rules = await this.client.listAlertingRules(this.orgName, this.appName);

      el.innerHTML = `
        <div class="toolbar">
          <div class="filter-group">
            <span class="text-secondary" style="font-size:12px;">Active Alert Rules</span>
          </div>
          <div>
            <button class="btn btn-sm btn-primary" onclick="window.app.openCreateAlertModal()">+ New Alert Rule</button>
          </div>
        </div>

        <div class="card">
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Rule ID</th>
                  <th>Type</th>
                  <th>Min Interval</th>
                  <th>Metadata</th>
                  <th>Last Fired</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                ${rules.length > 0 ? rules.map(r => `
                  <tr>
                    <td><code>${escapeHtml(r.id)}</code></td>
                    <td><strong>${escapeHtml(r.ruleType)}</strong></td>
                    <td>${r.minIntervalSecs || 0}s</td>
                    <td><code>${escapeHtml(JSON.stringify(r.ruleMetadata || {}))}</code></td>
                    <td>${formatTimestamp(r.lastFiredAt)}</td>
                    <td>
                      <button class="btn btn-xs btn-danger" onclick="window.app.deleteAlertRule('${escapeHtml(r.id)}')">Delete</button>
                    </td>
                  </tr>
                `).join("") : `<tr><td colspan="6" style="text-align:center; color:var(--text-tertiary);">No alerting rules configured.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error);">Failed to load alert rules: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  openCreateAlertModal() {
    const root = document.getElementById("modal-root");
    if (!root) return;

    root.innerHTML = `
      <div class="modal-overlay" onclick="if(event.target === this) window.app.closeModal()">
        <div class="modal-dialog">
          <div class="modal-header">
            <span>Create Alert Rule</span>
            <button class="btn btn-xs btn-secondary" onclick="window.app.closeModal()">✕</button>
          </div>
          <div class="modal-body">
            <div class="form-field">
              <label class="form-label">Rule Type</label>
              <select id="new-rule-type" class="select-sm">
                <option value="WorkflowFailure">WorkflowFailure</option>
                <option value="SlowQueue">SlowQueue</option>
                <option value="UnresponsiveApplication">UnresponsiveApplication</option>
              </select>
            </div>
            <div class="form-field">
              <label class="form-label">Min Interval (seconds)</label>
              <input type="number" id="new-rule-interval" class="input-text" value="300" min="0">
            </div>
            <div class="form-field">
              <label class="form-label">Rule Metadata (JSON)</label>
              <textarea id="new-rule-meta" class="input-text" rows="4" style="font-family:var(--font-mono); font-size:12px;">{"threshold": 10}</textarea>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-secondary" onclick="window.app.closeModal()">Cancel</button>
            <button class="btn btn-sm btn-primary" onclick="window.app.submitCreateAlert()">Create Rule</button>
          </div>
        </div>
      </div>
    `;
  }

  async submitCreateAlert() {
    const ruleType = document.getElementById("new-rule-type").value;
    const minIntervalSecs = parseInt(document.getElementById("new-rule-interval").value, 10) || 0;
    let ruleMetadata = {};
    try {
      ruleMetadata = JSON.parse(document.getElementById("new-rule-meta").value);
    } catch {
      alert("Invalid JSON in rule metadata");
      return;
    }

    try {
      await this.client.createAlertingRule(this.orgName, this.appName, {
        ruleType,
        minIntervalSecs,
        ruleMetadata,
      });
      this.closeModal();
      this.renderContentView();
    } catch (err) {
      alert("Failed to create rule: " + err.message);
    }
  }

  async deleteAlertRule(ruleId) {
    if (!confirm("Delete this alerting rule?")) return;
    try {
      await this.client.deleteAlertingRule(this.orgName, this.appName, ruleId);
      this.renderContentView();
    } catch (err) {
      alert("Failed to delete rule: " + err.message);
    }
  }

  // --- SCREEN 7: API KEYS ---
  async renderKeysScreen(el) {
    el.innerHTML = `<div class="loading-spinner">Loading API keys...</div>`;

    try {
      const keys = await this.client.listAPIKeys(this.orgName);

      el.innerHTML = `
        <div class="toolbar">
          <div class="filter-group">
            <span class="text-secondary" style="font-size:12px;">Organization API Keys (${this.orgName})</span>
          </div>
          <div>
            <button class="btn btn-sm btn-primary" onclick="window.app.openCreateKeyModal()">+ Mint API Key</button>
          </div>
        </div>

        <div class="card">
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Key Name</th>
                  <th>Lookup Prefix</th>
                  <th>Scoped Apps</th>
                  <th>Created At</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                ${keys.length > 0 ? keys.map(k => `
                  <tr>
                    <td><strong>${escapeHtml(k.name || k.id)}</strong></td>
                    <td><code>${escapeHtml(k.lookup || "-")}</code></td>
                    <td>${k.applicationNames && k.applicationNames.length > 0 ? escapeHtml(k.applicationNames.join(", ")) : "All Applications"}</td>
                    <td>${formatTimestamp(k.createdAt)}</td>
                    <td>
                      <button class="btn btn-xs btn-danger" onclick="window.app.revokeKey('${escapeHtml(k.name || k.id)}')">Revoke</button>
                    </td>
                  </tr>
                `).join("") : `<tr><td colspan="5" style="text-align:center; color:var(--text-tertiary);">No API keys found.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error);">Failed to load API keys: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  openCreateKeyModal() {
    const root = document.getElementById("modal-root");
    if (!root) return;

    root.innerHTML = `
      <div class="modal-overlay" onclick="if(event.target === this) window.app.closeModal()">
        <div class="modal-dialog">
          <div class="modal-header">
            <span>Mint Scoped API Key</span>
            <button class="btn btn-xs btn-secondary" onclick="window.app.closeModal()">✕</button>
          </div>
          <div class="modal-body">
            <div class="form-field">
              <label class="form-label">Key Name</label>
              <input type="text" id="new-key-name" class="input-text" placeholder="e.g. ci-deploy-key">
            </div>
            <div class="form-field">
              <label class="form-label">Application Scope</label>
              <select id="new-key-app" class="select-sm">
                <option value="">All Applications</option>
                ${this.apps.map(a => `<option value="${escapeHtml(a.name)}">${escapeHtml(a.name)}</option>`).join("")}
              </select>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-secondary" onclick="window.app.closeModal()">Cancel</button>
            <button class="btn btn-sm btn-primary" onclick="window.app.submitCreateKey()">Mint Key</button>
          </div>
        </div>
      </div>
    `;
  }

  async submitCreateKey() {
    const name = document.getElementById("new-key-name").value.trim();
    if (!name) {
      alert("Key name is required");
      return;
    }
    const appScope = document.getElementById("new-key-app").value;
    const appNames = appScope ? [appScope] : [];

    try {
      const res = await this.client.createAPIKey(this.orgName, name, ["*"], appNames);
      this.renderKeyCreatedModal(name, res.token);
    } catch (err) {
      alert("Failed to create key: " + err.message);
    }
  }

  renderKeyCreatedModal(name, token) {
    const root = document.getElementById("modal-root");
    if (!root) return;

    root.innerHTML = `
      <div class="modal-overlay">
        <div class="modal-dialog">
          <div class="modal-header">
            <span>API Key Created</span>
          </div>
          <div class="modal-body">
            <p style="font-size:13px; color:var(--color-warning);">
              Please copy this key now. It will not be shown again.
            </p>
            <div class="json-viewer" style="padding:12px; margin-top:8px;">
              <code style="word-break:break-all; font-weight:700;">${escapeHtml(token)}</code>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-primary" onclick="navigator.clipboard.writeText('${escapeHtml(token)}').then(() => { window.app.closeModal(); window.app.renderContentView(); })">Copy & Done</button>
          </div>
        </div>
      </div>
    `;
  }

  async revokeKey(name) {
    if (!confirm(`Revoke API key "${name}"?`)) return;
    try {
      await this.client.revokeAPIKey(this.orgName, name);
      this.renderContentView();
    } catch (err) {
      alert("Failed to revoke key: " + err.message);
    }
  }
}

// Helpers
function escapeHtml(str) {
  if (str === null || str === undefined) return "";
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

function formatTimestamp(isoStr) {
  if (!isoStr) return "-";
  try {
    const d = new Date(isoStr);
    return d.toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return isoStr;
  }
}

function calculateDuration(startStr, endStr) {
  if (!startStr || !endStr) return "-";
  try {
    const start = new Date(startStr).getTime();
    const end = new Date(endStr).getTime();
    const diffMs = Math.max(0, end - start);
    if (diffMs < 1000) return `${diffMs}ms`;
    if (diffMs < 60000) return `${(diffMs / 1000).toFixed(1)}s`;
    return `${Math.floor(diffMs / 60000)}m ${Math.floor((diffMs % 60000) / 1000)}s`;
  } catch {
    return "-";
  }
}

// Initialize on DOM load
window.addEventListener("DOMContentLoaded", () => {
  window.app = new DashboardApp();
  window.app.init();
});
