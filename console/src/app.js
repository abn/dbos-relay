// Relay Dashboard SPA Application

import { ApiClient } from "./lib/api/client.js";
import { renderStatusPill } from "./lib/components/StatusPill.js";
import { renderJsonViewer } from "./lib/components/JsonViewer.js";
import { renderWorkflowDAG } from "./lib/components/WorkflowDAG.js";

class DashboardApp {
  constructor() {
    this.apiKey = sessionStorage.getItem("relay_api_key") || null;
    this.client = new ApiClient("", this.apiKey);
    this.currentRoute = "fleet";
    this.orgName = "default";
    this.appName = "";
    this.apps = [];
    this.selectedWorkflowId = null;
    this.selectedStep = null;
    this.theme = localStorage.getItem("relay-theme") || (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
    this.pollInterval = 0;
    this.pollTimer = null;
    this.mobileNavOpen = false;
    this.pendingConfirmAction = null;
    this.parentWorkflowMap = new Map();
    this.currentWorkflowName = "";
    this.currentWorkflowStatus = "";
    this.dagZoom = 1;
    this.dagPanX = 0;
    this.dagPanY = 0;
    this.isPanningDag = false;
    this.panStartX = 0;
    this.panStartY = 0;

    // Expose global callback for SVG DAG node clicks
    window.selectStep = (encodedStepJson) => {
      try {
        let step;
        try {
          step = JSON.parse(encodedStepJson);
        } catch {
          step = JSON.parse(decodeURIComponent(encodedStepJson));
        }
        this.selectedStep = step;
        this.renderStepDrawer(step);
      } catch (err) {
        console.error("Failed to parse step:", err);
      }
    };
  }

  setApiKey(key) {
    this.apiKey = key;
    this.client.setApiKey(key);
    if (key) {
      sessionStorage.setItem("relay_api_key", key);
    } else {
      sessionStorage.removeItem("relay_api_key");
    }
  }

  isAuthError(err) {
    if (!err) return false;
    const msg = err.message || String(err);
    return msg.includes("401") || msg.includes("Unauthorized") || msg.includes("Authorization header required");
  }

  submitSignIn(token) {
    this.setApiKey(token || null);
    this.closeModal();
    if (token) {
      this.showToast("Saved credential successfully", "success");
    } else {
      this.showToast("Cleared saved credential", "info");
    }
    this.render();
  }

  signOut() {
    this.setApiKey(null);
    this.closeModal();
    this.showToast("Signed out. Using unauthenticated public access", "info");
    this.render();
  }

  showToast(message, type = "info") {
    let container = document.getElementById("toast-container");
    if (!container) {
      container = document.createElement("div");
      container.id = "toast-container";
      container.setAttribute("role", "status");
      container.setAttribute("aria-live", "polite");
      document.body.appendChild(container);
    }
    const toast = document.createElement("div");
    toast.className = `toast toast-${type}`;
    const icon = type === "success"
      ? `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polyline points="20 6 9 17 4 12"/></svg>`
      : type === "error"
      ? `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>`
      : `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>`;
    toast.innerHTML = `<span aria-hidden="true" style="display:inline-flex; align-items:center;">${icon}</span><span>${escapeHtml(message)}</span>`;
    container.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = "0";
      toast.style.transform = "translateY(10px)";
      toast.style.transition = "all 0.2s ease";
      setTimeout(() => toast.remove(), 250);
    }, 3500);
  }

  showConfirm({ title, message, consequence, details = [], confirmText = "Confirm", confirmClass = "btn-danger", onConfirm }) {
    const root = document.getElementById("modal-root");
    if (!root) return;

    this.pendingConfirmAction = onConfirm;

    root.innerHTML = `
      <div class="modal-overlay" data-action="closeModalOverlay" role="alertdialog" aria-modal="true" aria-labelledby="confirm-dialog-title" aria-describedby="confirm-dialog-desc">
        <div class="modal-dialog">
          <div class="modal-header">
            <span id="confirm-dialog-title">${escapeHtml(title)}</span>
            <button class="btn btn-xs btn-secondary" data-action="closeModal" aria-label="Cancel">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="modal-body">
            <p id="confirm-dialog-desc" style="font-size: 13px; color: var(--text-primary);">
              ${escapeHtml(message)}
            </p>

            ${details.length > 0 ? `
              <table class="confirm-meta-table">
                <tbody>
                  ${details.map(d => `
                    <tr>
                      <td>${escapeHtml(d.label)}</td>
                      <td><code>${escapeHtml(d.value)}</code></td>
                    </tr>
                  `).join("")}
                </tbody>
              </table>
            ` : ""}

            ${consequence ? `
              <div class="confirm-consequence" role="alert">
                <strong>Warning:</strong> ${escapeHtml(consequence)}
              </div>
            ` : ""}
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-secondary" id="confirm-cancel-btn" data-action="closeModal">Cancel</button>
            <button class="btn btn-sm ${confirmClass}" id="confirm-action-btn" data-action="executePendingConfirm">${escapeHtml(confirmText)}</button>
          </div>
        </div>
      </div>
    `;

    const cancelBtn = document.getElementById("confirm-cancel-btn");
    if (cancelBtn) cancelBtn.focus();
  }

  async executePendingConfirm() {
    const action = this.pendingConfirmAction;
    this.pendingConfirmAction = null;
    this.closeModal();
    if (typeof action === "function") {
      await action();
    }
  }

  renderAuthRequired(el, actionName = "load data") {
    el.innerHTML = `
      <div class="card">
        <div class="card-header">
          <span class="card-title">Authentication Required</span>
        </div>
        <div class="card-body">
          <p style="margin-bottom: 12px; color: var(--text-secondary);">
            This Relay instance requires an API key or bearer token to ${escapeHtml(actionName)}.
          </p>
          <div class="form-field" style="max-width: 480px;">
            <label class="form-label" for="auth-key-input">API Key / Bearer Token</label>
            <input type="password" id="auth-key-input" class="input-text" placeholder="dbos_sec_... or JWT token" value="${escapeHtml(this.apiKey || "")}">
          </div>
          <div style="margin-top: 16px; display: flex; gap: 8px;">
            <button class="btn btn-sm btn-primary" data-action="submitSignIn">Save & Retry</button>
            ${this.apiKey ? `<button class="btn btn-sm btn-secondary" data-action="signOut">Clear Credential</button>` : ""}
          </div>
        </div>
      </div>
    `;
  }

  openSignInModal() {
    const root = document.getElementById("modal-root");
    if (!root) return;

    root.innerHTML = `
      <div class="modal-overlay" data-action="closeModalOverlay" role="dialog" aria-modal="true" aria-labelledby="signin-modal-title">
        <div class="modal-dialog">
          <div class="modal-header">
            <span id="signin-modal-title">Relay Authentication</span>
            <button class="btn btn-xs btn-secondary" data-action="closeModal" aria-label="Close dialog">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="modal-body">
            <p style="font-size: 13px; color: var(--text-secondary); margin-bottom: 12px;">
              Provide an API key or OIDC bearer token to authenticate with the Relay control plane.
            </p>
            <div class="form-field">
              <label class="form-label" for="modal-auth-key-input">API Key / Bearer Token</label>
              <input type="password" id="modal-auth-key-input" class="input-text" placeholder="dbos_sec_... or JWT token" value="${escapeHtml(this.apiKey || "")}">
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-secondary" data-action="closeModal">Cancel</button>
            <button class="btn btn-sm btn-primary" data-action="submitModalSignIn">Sign In</button>
          </div>
        </div>
      </div>
    `;
    const input = document.getElementById("modal-auth-key-input");
    if (input) input.focus();
  }

  setPollInterval(ms) {
    this.pollInterval = parseInt(ms, 10) || 0;
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = null;
    }
    if (this.pollInterval > 0) {
      this.pollTimer = setInterval(() => this.poll(), this.pollInterval);
    }
    const dot = document.querySelector(".poll-dot");
    if (dot) {
      if (this.pollInterval > 0) dot.classList.add("active");
      else dot.classList.remove("active");
    }
  }

  async poll() {
    const modal = document.getElementById("modal-root");
    if (modal && modal.children.length > 0) return;
    const active = document.activeElement;
    if (active && (active.tagName === "INPUT" || active.tagName === "SELECT" || active.tagName === "TEXTAREA")) return;

    await this.renderContentView(true);
  }

  toggleMobileNav() {
    this.mobileNavOpen = !this.mobileNavOpen;
    const sidebar = document.querySelector(".sidebar");
    const overlay = document.querySelector(".mobile-overlay");
    if (sidebar) sidebar.classList.toggle("mobile-open", this.mobileNavOpen);
    if (overlay) overlay.classList.toggle("active", this.mobileNavOpen);
  }

  closeMobileNav() {
    this.mobileNavOpen = false;
    const sidebar = document.querySelector(".sidebar");
    const overlay = document.querySelector(".mobile-overlay");
    if (sidebar) sidebar.classList.remove("mobile-open");
    if (overlay) overlay.classList.remove("active");
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
      this.selectedWorkflowId = decodeURIComponent(parts[1]);
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
      <div class="mobile-overlay ${this.mobileNavOpen ? 'active' : ''}" data-action="closeMobileNav"></div>
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
      <aside class="sidebar ${this.mobileNavOpen ? 'mobile-open' : ''}">
        <div class="sidebar-header">
          <a href="#/fleet" class="brand-logo" aria-label="Relay Home">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M13 2L3 14h9l-1 8 10-12h-9l1-8z" fill="var(--color-primary)" stroke="none"/>
            </svg>
            <span>Relay</span>
          </a>
          <span class="brand-badge">Dashboard</span>
        </div>
        <nav class="sidebar-nav" aria-label="Main Navigation">
          ${navItems.map(item => `
            <a href="#/${item.id}"
               class="nav-item ${this.currentRoute === item.id || (this.currentRoute === 'workflow-detail' && item.id === 'workflows') ? 'active' : ''}"
               data-navigate="${item.id}">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
                ${item.icon}
              </svg>
              <span>${item.label}</span>
            </a>
          `).join("")}
        </nav>
        <div class="sidebar-footer">
          <span>Relay Control Plane</span>
          <button class="btn btn-xs btn-secondary" data-action="toggleTheme" aria-label="Toggle dark and light theme">
            ${this.theme === "dark" ? `
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
              <span style="margin-left:4px;">Light</span>
            ` : `
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
              <span style="margin-left:4px;">Dark</span>
            `}
          </button>
        </div>
      </aside>
    `;
  }

  renderTopHeader() {
    const hasKey = Boolean(this.apiKey);
    return `
      <header class="top-header">
        <div class="header-left">
          <button class="mobile-nav-toggle" data-action="toggleMobileNav" aria-label="Toggle navigation menu">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M4 6h16M4 12h16M4 18h16"/>
            </svg>
          </button>
          <h1 class="header-title">${this.getRouteTitle()}</h1>
        </div>
        <div class="header-right">
          <div class="selector-group">
            <label class="form-label" for="header-app-select" style="margin:0;">App:</label>
            <select id="header-app-select" class="select-sm" data-change="app" aria-label="Active application">
              ${this.apps.map(a => `<option value="${escapeHtml(a.name)}" ${a.name === this.appName ? "selected" : ""}>${escapeHtml(a.name)}</option>`).join("")}
              ${this.apps.length === 0 ? `<option value="">No applications</option>` : ""}
            </select>
          </div>
          <div class="selector-group">
            <span class="poll-indicator" title="Live telemetry refresh">
              <span class="poll-dot ${this.pollInterval > 0 ? 'active' : ''}"></span>
              <span>Poll:</span>
            </span>
            <select class="select-sm" data-change="pollInterval" aria-label="Live polling interval">
              <option value="0" ${this.pollInterval === 0 ? "selected" : ""}>Off</option>
              <option value="2000" ${this.pollInterval === 2000 ? "selected" : ""}>2s</option>
              <option value="5000" ${this.pollInterval === 5000 ? "selected" : ""}>5s</option>
              <option value="15000" ${this.pollInterval === 15000 ? "selected" : ""}>15s</option>
            </select>
          </div>
          <button class="btn btn-xs btn-secondary" data-action="refresh" aria-label="Refresh telemetry (Shortcut: R)">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67"/></svg>
            <span style="margin-left:4px;">Refresh</span>
            <span class="hotkey-badge">R</span>
          </button>
          ${hasKey ? `
            <button class="btn btn-xs btn-secondary" data-action="signOut" title="Clear saved credential">Sign Out</button>
          ` : `
            <button class="btn btn-xs btn-primary" data-action="openSignIn" title="Set API Key or Bearer Token">Sign In</button>
          `}
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
    const select = document.getElementById("header-app-select");
    if (select) select.value = newAppName;
    this.renderContentView();
  }

  async renderContentView(silent = false) {
    const el = document.getElementById("content-view");
    if (!el) return;

    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading...</div>`;
    }

    switch (this.currentRoute) {
      case "fleet":
        await this.renderFleetScreen(el, silent);
        break;
      case "workflows":
        await this.renderWorkflowsScreen(el, silent);
        break;
      case "workflow-detail":
        await this.renderWorkflowDetailScreen(el, silent);
        break;
      case "queues":
        await this.renderQueuesScreen(el, silent);
        break;
      case "schedules":
        await this.renderSchedulesScreen(el, silent);
        break;
      case "alerting":
        await this.renderAlertingScreen(el, silent);
        break;
      case "keys":
        await this.renderKeysScreen(el, silent);
        break;
      default:
        el.innerHTML = `<div class="card"><div class="card-body">Select a view from the sidebar.</div></div>`;
    }
  }

  // --- SCREEN 1: FLEET & APPLICATIONS ---
  async renderFleetScreen(el, silent = false) {
    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading fleet overview...</div>`;
    }

    try {
      await this.loadApplications();
      let executors = [];
      let workflows = [];
      let queues = [];
      let schedules = [];

      if (this.appName) {
        const [execRes, wfRes, qRes, schRes] = await Promise.allSettled([
          this.client.listExecutors(this.orgName, this.appName),
          this.client.listWorkflows(this.orgName, this.appName, { limit: 100 }),
          this.client.listQueues ? this.client.listQueues(this.orgName, this.appName) : Promise.resolve([]),
          this.client.listSchedules ? this.client.listSchedules(this.orgName, this.appName) : Promise.resolve([])
        ]);

        if (execRes.status === "fulfilled") executors = execRes.value || [];
        if (wfRes.status === "fulfilled") workflows = wfRes.value || [];
        if (qRes.status === "fulfilled") queues = qRes.value || [];
        if (schRes.status === "fulfilled") schedules = schRes.value || [];
      }

      const activeExecutors = executors.filter(e => e.status === "HEALTHY").length;
      const disconnectedExecutors = executors.filter(e => e.status === "DISCONNECTED").length;

      const inFlightWfs = workflows.filter(w => w.status === "PENDING" || w.status === "ENQUEUED").length;
      const failedWfs = workflows.filter(w => w.status === "ERROR").length;

      const recentWorkflows = [...workflows]
        .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
        .slice(0, 8);

      el.innerHTML = `
        <div class="stat-grid">
          <div class="stat-card">
            <span class="stat-label">Workflows</span>
            <span class="stat-value">${workflows.length}</span>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill kpi-info">${inFlightWfs} in flight</span>
              <span class="kpi-sub-pill ${failedWfs > 0 ? 'kpi-error' : 'kpi-success'}">${failedWfs} errors</span>
            </div>
          </div>
          <div class="stat-card">
            <span class="stat-label">Connected Executors</span>
            <span class="stat-value" style="color: var(--color-success-text);">${activeExecutors}</span>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill kpi-success">${activeExecutors} healthy</span>
              ${disconnectedExecutors > 0 ? `<span class="kpi-sub-pill kpi-error">${disconnectedExecutors} disconnected</span>` : `<span class="kpi-sub-pill">0 disconnected</span>`}
            </div>
          </div>
          <div class="stat-card">
            <span class="stat-label">Active Queues & Schedules</span>
            <span class="stat-value">${queues.length + schedules.length}</span>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill">${queues.length} queues</span>
              <span class="kpi-sub-pill">${schedules.length} schedules</span>
            </div>
          </div>
          <div class="stat-card">
            <span class="stat-label">Applications</span>
            <span class="stat-value">${this.apps.length}</span>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill">${escapeHtml(this.appName || "None")}</span>
            </div>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <div>
              <span class="card-title">Recent Workflow Executions</span>
              <span style="font-size:12px; color:var(--text-secondary); margin-left:8px;">Live operational telemetry</span>
            </div>
            <a href="#/workflows" class="btn btn-xs btn-secondary" data-navigate="workflows">View All Workflows →</a>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Status</th>
                  <th>Workflow ID</th>
                  <th>Name</th>
                  <th>Queue</th>
                  <th>Started</th>
                  <th>Duration</th>
                  <th>Action</th>
                </tr>
              </thead>
              <tbody>
                ${recentWorkflows.length > 0 ? recentWorkflows.map(w => `
                  <tr class="clickable" tabindex="0" role="button" aria-label="View workflow ${escapeHtml(w.workflowId)}" data-navigate="workflow/${escapeHtml(w.workflowId)}">
                    <td>${renderStatusPill(w.status)}</td>
                    <td><code>${escapeHtml(truncate(w.workflowId, 22))}</code></td>
                    <td><strong>${escapeHtml(w.workflowName || "unnamed")}</strong></td>
                    <td>${escapeHtml(w.queueName || "default")}</td>
                    <td>
                      <span class="relative-time" title="${formatTimestamp(w.createdAt)}">
                        ${formatRelativeTime(w.createdAt)}
                      </span>
                    </td>
                    <td>${calculateDuration(w.createdAt, w.completedAt)}</td>
                    <td>
                      <a href="#/workflow/${escapeHtml(w.workflowId)}" class="btn btn-xs btn-secondary" data-navigate="workflow/${escapeHtml(w.workflowId)}">Inspect ↗</a>
                    </td>
                  </tr>
                `).join("") : `<tr><td colspan="7" style="text-align:center; color:var(--text-tertiary); padding:24px;">No workflow executions recorded for this application.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <span class="card-title">Connected Executors (${escapeHtml(this.appName || 'No app')})</span>
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
                  <tr class="clickable" tabindex="0" role="button" aria-label="Select application ${escapeHtml(a.name)}" data-app-change='${escapeHtml(a.name)}'>
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
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error-text);">Failed to load fleet: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  // --- SCREEN 2: WORKFLOW SEARCH & LIST ---
  async renderWorkflowsScreen(el, silent = false) {
    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading workflows...</div>`;
    }

    try {
      const workflows = await this.client.listWorkflows(this.orgName, this.appName, { limit: 50 });

      el.innerHTML = `
        <div class="toolbar">
          <div class="filter-group">
            <input type="text" id="filter-id" class="input-text" placeholder="Search workflows... [/]" data-input="wfTable" aria-label="Search workflows by ID or name">
            <select id="filter-status" class="select-sm" data-change="wfStatus" aria-label="Filter workflows by status">
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
                  <tr class="clickable" tabindex="0" role="button" aria-label="View workflow ${escapeHtml(wf.workflowId)}" data-navigate='workflow/${encodeURIComponent(wf.workflowId)}'>
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
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error-text);">Failed to load workflows: ${escapeHtml(err.message)}</div></div>`;
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
  async renderWorkflowDetailScreen(el, silent = false) {
    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading workflow details...</div>`;
    }

    try {
      const wf = await this.client.getWorkflow(this.orgName, this.appName, this.selectedWorkflowId);
      const steps = await this.client.listSteps(this.orgName, this.appName, this.selectedWorkflowId);

      this.currentWorkflowName = wf.workflowName || wf.workflowId;
      this.currentWorkflowStatus = wf.status;

      // Check for child workflows spawned by any step
      const childStepEntries = steps.filter(s => Boolean(s.childWorkflowId));
      let childWorkflows = [];

      if (childStepEntries.length > 0) {
        const childResults = await Promise.allSettled(
          childStepEntries.map(async (step) => {
            try {
              const childWf = await this.client.getWorkflow(this.orgName, this.appName, step.childWorkflowId);
              const childSteps = await this.client.listSteps(this.orgName, this.appName, step.childWorkflowId);
              return {
                stepId: step.stepId,
                childWorkflowId: step.childWorkflowId,
                workflow: childWf,
                steps: childSteps || []
              };
            } catch {
              return {
                stepId: step.stepId,
                childWorkflowId: step.childWorkflowId,
                workflow: { workflowId: step.childWorkflowId, status: "UNKNOWN", workflowName: "Child Workflow" },
                steps: []
              };
            }
          })
        );
        childWorkflows = childResults.filter(r => r.status === "fulfilled").map(r => r.value);
      }

      const familyData = {
        root: { workflow: wf, steps },
        children: childWorkflows
      };

      const parentInfo = this.parentWorkflowMap.get(this.selectedWorkflowId);
      const statusDotClass = wf.status === "SUCCESS" ? "dot-success" : (wf.status === "ERROR" ? "dot-error" : (wf.status === "PENDING" || wf.status === "ENQUEUED" ? "dot-running" : "dot-pending"));
      const parentStatusDotClass = parentInfo ? (parentInfo.parentStatus === "SUCCESS" ? "dot-success" : (parentInfo.parentStatus === "ERROR" ? "dot-error" : "dot-running")) : "dot-pending";

      const breadcrumbsHtml = `
        <nav class="wf-breadcrumbs" aria-label="Workflow Navigation Breadcrumbs">
          <a href="#/workflows" class="wf-breadcrumb-link" data-navigate="workflows" aria-label="All workflows">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polyline points="15 18 9 12 15 6"/></svg>
            <span>Workflows</span>
          </a>
          <span class="wf-breadcrumb-separator" aria-hidden="true">/</span>
          ${parentInfo ? `
            <a href="#/workflow/${encodeURIComponent(parentInfo.parentId)}" class="wf-breadcrumb-link" data-navigate="workflow/${encodeURIComponent(parentInfo.parentId)}" aria-label="Parent workflow ${escapeHtml(parentInfo.parentName)}">
              <span class="status-dot ${parentStatusDotClass}"></span>
              <span>${escapeHtml(truncate(parentInfo.parentName || parentInfo.parentId, 20))}</span>
            </a>
            <span class="wf-breadcrumb-separator" aria-hidden="true">/</span>
          ` : ""}
          <span class="wf-breadcrumb-current" aria-current="page">
            <span class="status-dot ${statusDotClass}"></span>
            <span class="wf-breadcrumb-name">${escapeHtml(wf.workflowName || wf.workflowId)}</span>
          </span>
        </nav>
      `;

      el.innerHTML = `
        ${breadcrumbsHtml}

        <div class="card">
          <div class="card-header">
            <div style="display:flex; align-items:center; gap:12px;">
              <span class="card-title">Workflow: <code>${escapeHtml(wf.workflowId)}</code></span>
              ${renderStatusPill(wf.status)}
            </div>
            <div style="display:flex; gap:8px;">
              ${wf.status === "PENDING" || wf.status === "ENQUEUED" ? `
                <button class="btn btn-sm btn-danger" data-cancel-wf='${escapeHtml(wf.workflowId)}'>Cancel</button>
              ` : ""}
              ${wf.status === "CANCELLED" ? `
                <button class="btn btn-sm btn-primary" data-resume-wf='${escapeHtml(wf.workflowId)}'>Resume</button>
              ` : ""}
              ${wf.status === "ERROR" ? `
                <button class="btn btn-sm btn-primary" data-restart-wf='${escapeHtml(wf.workflowId)}'>Restart</button>
              ` : ""}
            </div>
          </div>
          <div class="card-body">
            <div class="stat-grid" style="grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); margin-bottom: 0;">
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
            <span class="card-title">Execution Steps (${steps.length}${childWorkflows.length > 0 ? ` + ${childWorkflows.length} child workflows` : ""})</span>
            <span style="font-size:12px; color:var(--text-secondary);">Click any step to inspect parameters, output, or child executions</span>
          </div>
          <div class="card-body" style="padding:0;">
            ${renderWorkflowDAG(familyData, "window.selectStep")}
          </div>
        </div>

        <div class="card">
          <div class="tab-list">
            <button class="tab-btn active" id="tab-btn-io" data-wf-tab='io'>Inputs & Outputs</button>
            <button class="tab-btn" id="tab-btn-events" data-wf-tab='events'>Events</button>
            <button class="tab-btn" id="tab-btn-notifications" data-wf-tab='notifications'>Notifications</button>
          </div>
          <div class="card-body" id="wf-tab-content">
            <div style="display:grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px;">
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

      this.initDagViewport();
    } catch (err) {
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error-text);">Failed to load workflow: ${escapeHtml(err.message)}</div></div>`;
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
        <div style="display:grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px;">
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
    this.showConfirm({
      title: "Cancel Workflow Execution",
      message: `Are you sure you want to cancel workflow ${id}?`,
      consequence: "Execution will halt immediately. In-flight and pending steps will be cancelled and cannot be resumed without explicit operator intervention.",
      details: [
        { label: "Workflow ID", value: id },
        { label: "Application", value: this.appName || "default" },
        { label: "Transition", value: "-> CANCELLED" }
      ],
      confirmText: "Cancel Workflow",
      confirmClass: "btn-danger",
      onConfirm: async () => {
        try {
          await this.client.cancelWorkflow(this.orgName, this.appName, id);
          this.showToast(`Workflow ${id} cancelled`, "success");
          this.renderWorkflowDetailScreen(document.getElementById("content-view"));
        } catch (err) {
          this.showToast(`Failed to cancel: ${err.message}`, "error");
        }
      }
    });
  }

  async resumeWorkflow(id) {
    try {
      await this.client.resumeWorkflow(this.orgName, this.appName, id);
      this.showToast(`Resumed workflow ${id}`, "success");
      this.renderWorkflowDetailScreen(document.getElementById("content-view"));
    } catch (err) {
      this.showToast(`Failed to resume: ${err.message}`, "error");
    }
  }

  async restartWorkflow(id) {
    try {
      const res = await this.client.forkWorkflow(this.orgName, this.appName, id, 0);
      this.showToast(`Restarted workflow! New ID: ${res.workflowId}`, "success");
      this.navigate(`workflow/${encodeURIComponent(res.workflowId)}`);
    } catch (err) {
      this.showToast(`Failed to restart: ${err.message}`, "error");
    }
  }

  renderStepDrawer(step) {
    const root = document.getElementById("modal-root");
    if (!root) return;

    root.innerHTML = `
      <div class="drawer-overlay" data-action="closeModalOverlay" role="dialog" aria-modal="true" aria-labelledby="drawer-step-title">
        <aside class="drawer-panel" role="document">
          <div class="drawer-header">
            <span id="drawer-step-title">Step #${step.stepId}: ${escapeHtml(step.stepName)}</span>
            <button class="btn btn-xs btn-secondary" data-action="closeModal" aria-label="Close inspector">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="drawer-body">
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
                <div style="margin-top:4px;">
                  <a class="btn btn-xs btn-primary" data-action="viewChildWorkflow" data-child-wf-id="${escapeHtml(step.childWorkflowId)}">
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
                <span class="stat-label" style="color:var(--color-error-text);">Error Stack</span>
                ${renderJsonViewer(step.error, "Error Details")}
              </div>
            ` : ""}
          </div>
          <div class="drawer-footer">
            <button class="btn btn-sm btn-secondary" data-action="closeModal">Close</button>
          </div>
        </aside>
      </div>
    `;

    const closeBtn = root.querySelector(".drawer-header button");
    if (closeBtn) closeBtn.focus();
  }

  renderStepModal(step) {
    this.renderStepDrawer(step);
  }

  closeModal() {
    const root = document.getElementById("modal-root");
    if (root) root.innerHTML = "";
  }

  openExpandPayloadModal(title, raw) {
    const root = document.getElementById("modal-root");
    if (!root) return;

    let parsed = null;
    let isJson = false;
    try {
      parsed = JSON.parse(raw);
      isJson = true;
    } catch {
      isJson = false;
    }

    const formatted = isJson ? JSON.stringify(parsed, null, 2) : raw;

    root.innerHTML = `
      <div class="modal-overlay" data-action="closeModalOverlay" role="dialog" aria-modal="true" aria-labelledby="payload-modal-title">
        <div class="modal-dialog modal-dialog-large" role="document">
          <div class="modal-header">
            <h3 class="modal-title" id="payload-modal-title">${escapeHtml(title || "Payload Details")}</h3>
            <button class="btn btn-xs btn-secondary" data-action="closeModal" aria-label="Close dialog">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="modal-body">
            <div class="json-viewer" id="modal-expanded-viewer">
              <div class="json-header">
                <span class="json-title">${escapeHtml(title)}</span>
                <button class="btn btn-xs btn-secondary copy-btn" data-copy="${escapeHtml(raw)}">Copy Payload</button>
              </div>
              <pre class="json-content" style="max-height: 55vh;"><code>${escapeHtml(formatted)}</code></pre>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-secondary" data-action="closeModal">Close</button>
          </div>
        </div>
      </div>
    `;

    const closeBtn = root.querySelector(".modal-header button");
    if (closeBtn) closeBtn.focus();
  }

  initDagViewport() {
    this.dagZoom = 1;
    this.dagPanX = 0;
    this.dagPanY = 0;
    this.isPanningDag = false;
    this.panStartX = 0;
    this.panStartY = 0;

    const viewport = document.getElementById("dag-viewport");
    if (!viewport) return;

    viewport.addEventListener("mousedown", (e) => {
      if (e.target.closest(".dag-node") || e.target.closest("button") || e.target.closest("a") || e.target.closest(".child-card-action")) return;
      this.isPanningDag = true;
      this.panStartX = e.clientX - this.dagPanX;
      this.panStartY = e.clientY - this.dagPanY;
    });

    window.addEventListener("mousemove", (e) => {
      if (!this.isPanningDag) return;
      this.dagPanX = e.clientX - this.panStartX;
      this.dagPanY = e.clientY - this.panStartY;
      this.updateDagTransform();
    });

    window.addEventListener("mouseup", () => {
      this.isPanningDag = false;
    });

    viewport.addEventListener("wheel", (e) => {
      e.preventDefault();
      const delta = e.deltaY < 0 ? 0.08 : -0.08;
      this.dagZoom = Math.min(2.5, Math.max(0.3, this.dagZoom + delta));
      this.updateDagTransform();
    }, { passive: false });
  }

  updateDagTransform() {
    const layer = document.getElementById("dag-zoom-layer");
    if (layer) {
      layer.setAttribute("transform", `translate(${this.dagPanX}, ${this.dagPanY}) scale(${this.dagZoom})`);
    }
  }

  zoomDag(delta) {
    this.dagZoom = Math.min(2.5, Math.max(0.3, this.dagZoom + delta));
    this.updateDagTransform();
  }

  resetDagZoom() {
    this.dagZoom = 1;
    this.dagPanX = 0;
    this.dagPanY = 0;
    this.updateDagTransform();
  }

  // --- SCREEN 4: QUEUES ---
  async renderQueuesScreen(el, silent = false) {
    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading queues...</div>`;
    }

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
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error-text);">Failed to load queues: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  // --- SCREEN 5: SCHEDULES ---
  async renderSchedulesScreen(el, silent = false) {
    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading schedules...</div>`;
    }

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
                          <button class="btn btn-xs btn-secondary" data-pause-schedule='${escapeHtml(s.scheduleName)}'>Pause</button>
                        ` : `
                          <button class="btn btn-xs btn-secondary" data-resume-schedule='${escapeHtml(s.scheduleName)}'>Resume</button>
                        `}
                        <button class="btn btn-xs btn-primary" data-trigger-schedule='${escapeHtml(s.scheduleName)}'>Trigger Now</button>
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
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error-text);">Failed to load schedules: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  async pauseSchedule(name) {
    try {
      await this.client.pauseSchedule(this.orgName, this.appName, name);
      this.showToast(`Schedule "${name}" paused`, "success");
      this.renderContentView();
    } catch (err) {
      this.showToast(`Failed to pause schedule: ${err.message}`, "error");
    }
  }

  async resumeSchedule(name) {
    try {
      await this.client.resumeSchedule(this.orgName, this.appName, name);
      this.showToast(`Schedule "${name}" resumed`, "success");
      this.renderContentView();
    } catch (err) {
      this.showToast(`Failed to resume schedule: ${err.message}`, "error");
    }
  }

  async triggerSchedule(name) {
    try {
      const res = await this.client.triggerSchedule(this.orgName, this.appName, name);
      this.showToast(`Triggered schedule "${name}". New Workflow: ${res.workflowId}`, "success");
      this.navigate(`workflow/${encodeURIComponent(res.workflowId)}`);
    } catch (err) {
      this.showToast(`Failed to trigger schedule: ${err.message}`, "error");
    }
  }

  // --- SCREEN 6: ALERTING RULES ---
  async renderAlertingScreen(el, silent = false) {
    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading alerting rules...</div>`;
    }

    try {
      const rules = await this.client.listAlertingRules(this.orgName, this.appName);

      el.innerHTML = `
        <div class="toolbar">
          <div class="filter-group">
            <span class="text-secondary" style="font-size:12px;">Active Alert Rules</span>
          </div>
          <div>
            <button class="btn btn-sm btn-primary" data-action="openCreateAlert">+ New Alert Rule</button>
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
                      <button class="btn btn-xs btn-danger" data-delete-rule='${escapeHtml(r.id)}'>Delete</button>
                    </td>
                  </tr>
                `).join("") : `<tr><td colspan="6" style="text-align:center; color:var(--text-tertiary);">No alerting rules configured.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error-text);">Failed to load alert rules: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  openCreateAlertModal() {
    const root = document.getElementById("modal-root");
    if (!root) return;

    root.innerHTML = `
      <div class="modal-overlay" data-action="closeModalOverlay" role="dialog" aria-modal="true" aria-labelledby="modal-alert-title">
        <div class="modal-dialog">
          <div class="modal-header">
            <span id="modal-alert-title">Create Alert Rule</span>
            <button class="btn btn-xs btn-secondary" data-action="closeModal" aria-label="Close dialog">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="modal-body">
            <div class="form-field">
              <label class="form-label" for="new-rule-type">Rule Type</label>
              <select id="new-rule-type" class="select-sm">
                <option value="WorkflowFailure">WorkflowFailure</option>
                <option value="SlowQueue">SlowQueue</option>
                <option value="UnresponsiveApplication">UnresponsiveApplication</option>
              </select>
            </div>
            <div class="form-field">
              <label class="form-label" for="new-rule-interval">Min Interval (seconds)</label>
              <input type="number" id="new-rule-interval" class="input-text" value="300" min="0">
            </div>
            <div class="form-field">
              <label class="form-label" for="new-rule-meta">Rule Metadata (JSON)</label>
              <textarea id="new-rule-meta" class="input-text" rows="4" style="font-family:var(--font-mono); font-size:12px;">{"threshold": 10}</textarea>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-secondary" data-action="closeModal">Cancel</button>
            <button class="btn btn-sm btn-primary" data-action="submitCreateAlert">Create Rule</button>
          </div>
        </div>
      </div>
    `;
    const typeSelect = document.getElementById("new-rule-type");
    if (typeSelect) typeSelect.focus();
  }

  async submitCreateAlert() {
    const ruleType = document.getElementById("new-rule-type").value;
    const minIntervalSecs = parseInt(document.getElementById("new-rule-interval").value, 10) || 0;
    let ruleMetadata = {};
    try {
      ruleMetadata = JSON.parse(document.getElementById("new-rule-meta").value);
    } catch {
      this.showToast("Invalid JSON in rule metadata", "error");
      return;
    }

    try {
      await this.client.createAlertingRule(this.orgName, this.appName, {
        ruleType,
        minIntervalSecs,
        ruleMetadata,
      });
      this.closeModal();
      this.showToast("Alert rule created successfully", "success");
      this.renderContentView();
    } catch (err) {
      this.showToast(`Failed to create rule: ${err.message}`, "error");
    }
  }

  async deleteAlertRule(ruleId) {
    this.showConfirm({
      title: "Delete Alert Rule",
      message: `Are you sure you want to delete alert rule "${ruleId}"?`,
      consequence: "Alert notifications defined by this rule will immediately stop firing. This action cannot be undone.",
      details: [
        { label: "Rule ID", value: ruleId },
        { label: "Application", value: this.appName || "default" },
        { label: "Action", value: "Permanent deletion" }
      ],
      confirmText: "Delete Rule",
      confirmClass: "btn-danger",
      onConfirm: async () => {
        try {
          await this.client.deleteAlertingRule(this.orgName, this.appName, ruleId);
          this.showToast("Alert rule deleted", "success");
          this.renderContentView();
        } catch (err) {
          this.showToast(`Failed to delete rule: ${err.message}`, "error");
        }
      }
    });
  }

  // --- SCREEN 7: API KEYS ---
  async renderKeysScreen(el, silent = false) {
    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading API keys...</div>`;
    }

    try {
      const keys = await this.client.listAPIKeys(this.orgName);

      el.innerHTML = `
        <div class="toolbar">
          <div class="filter-group">
            <span class="text-secondary" style="font-size:12px;">Organization API Keys (${this.orgName})</span>
          </div>
          <div>
            <button class="btn btn-sm btn-primary" data-action="openCreateKey">+ Mint API Key</button>
          </div>
        </div>

        <div class="card">
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Key Name</th>
                  <th>Permissions</th>
                  <th>Scoped Apps</th>
                  <th>Created At</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                ${keys.length > 0 ? keys.map(k => `
                  <tr>
                    <td><strong>${escapeHtml(k.tokenName)}</strong></td>
                    <td><code>${escapeHtml(k.permissions ? k.permissions.join(", ") : "")}</code></td>
                    <td>${k.appIds && k.appIds.length > 0 ? escapeHtml(k.appIds.join(", ")) : "All Applications"}</td>
                    <td>${formatTimestamp(k.createdAt)}</td>
                    <td>
                      <button class="btn btn-xs btn-danger" data-revoke-key='${escapeHtml(k.tokenName)}'>Revoke</button>
                    </td>
                  </tr>
                `).join("") : `<tr><td colspan="5" style="text-align:center; color:var(--text-tertiary);">No API keys found.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      if (this.isAuthError(err)) {
        this.renderAuthRequired(el, "load API keys");
        return;
      }
      el.innerHTML = `<div class="card"><div class="card-body" style="color:var(--color-error-text);">Failed to load API keys: ${escapeHtml(err.message)}</div></div>`;
    }
  }

  openCreateKeyModal() {
    const root = document.getElementById("modal-root");
    if (!root) return;

    root.innerHTML = `
      <div class="modal-overlay" data-action="closeModalOverlay" role="dialog" aria-modal="true" aria-labelledby="modal-key-title">
        <div class="modal-dialog">
          <div class="modal-header">
            <span id="modal-key-title">Mint Scoped API Key</span>
            <button class="btn btn-xs btn-secondary" data-action="closeModal" aria-label="Close dialog">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
          <div class="modal-body">
            <div class="form-field">
              <label class="form-label" for="new-key-name">Key Name</label>
              <input type="text" id="new-key-name" class="input-text" placeholder="e.g. ci-deploy-key">
            </div>
            <div class="form-field">
              <label class="form-label" for="new-key-app">Application Scope</label>
              <select id="new-key-app" class="select-sm">
                <option value="">All Applications</option>
                ${this.apps.map(a => `<option value="${escapeHtml(a.name)}">${escapeHtml(a.name)}</option>`).join("")}
              </select>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-secondary" data-action="closeModal">Cancel</button>
            <button class="btn btn-sm btn-primary" data-action="submitCreateKey">Mint Key</button>
          </div>
        </div>
      </div>
    `;
    const nameInput = document.getElementById("new-key-name");
    if (nameInput) nameInput.focus();
  }

  async submitCreateKey() {
    const name = document.getElementById("new-key-name").value.trim();
    if (!name) {
      this.showToast("Key name is required", "error");
      return;
    }
    const appScope = document.getElementById("new-key-app").value;
    const appNames = appScope ? [appScope] : [];

    try {
      const res = await this.client.createAPIKey(this.orgName, name, ["*"], appNames);
      this.renderKeyCreatedModal(name, res.token);
    } catch (err) {
      this.showToast(`Failed to create key: ${err.message}`, "error");
    }
  }

  renderKeyCreatedModal(name, token) {
    const root = document.getElementById("modal-root");
    if (!root) return;

    root.innerHTML = `
      <div class="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="modal-key-created-title">
        <div class="modal-dialog">
          <div class="modal-header">
            <span id="modal-key-created-title">API Key Created: ${escapeHtml(name)}</span>
          </div>
          <div class="modal-body">
            <p style="font-size:13px; color:var(--color-warning-text); font-weight:500;">
              Please copy this key now. It will not be shown again.
            </p>
            <div class="json-viewer" style="padding:12px; margin-top:8px;">
              <code style="word-break:break-all; font-weight:700;">${escapeHtml(token)}</code>
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn btn-sm btn-primary" data-copy-and-close='${escapeHtml(token)}'>Copy & Done</button>
          </div>
        </div>
      </div>
    `;
  }

  async revokeKey(name) {
    this.showConfirm({
      title: "Revoke API Key",
      message: `Are you sure you want to revoke API key "${name}"?`,
      consequence: "Any application, executor, or service authenticating with this key will immediately receive 401 Unauthorized errors.",
      details: [
        { label: "Key Name", value: name },
        { label: "Organization", value: this.orgName },
        { label: "Effect", value: "Immediate revocation" }
      ],
      confirmText: "Revoke Key",
      confirmClass: "btn-danger",
      onConfirm: async () => {
        try {
          await this.client.revokeAPIKey(this.orgName, name);
          this.showToast(`API key "${name}" revoked`, "success");
          this.renderContentView();
        } catch (err) {
          this.showToast(`Failed to revoke key: ${err.message}`, "error");
        }
      }
    });
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
  if (!startStr) return "-";
  try {
    const start = new Date(startStr).getTime();
    const isRunning = !endStr;
    const end = endStr ? new Date(endStr).getTime() : Date.now();
    const diffMs = Math.max(0, end - start);
    let str = "";
    if (diffMs < 1000) str = `${diffMs}ms`;
    else if (diffMs < 60000) str = `${(diffMs / 1000).toFixed(1)}s`;
    else str = `${Math.floor(diffMs / 60000)}m ${Math.floor((diffMs % 60000) / 1000)}s`;
    return isRunning ? `${str} (running)` : str;
  } catch {
    return "-";
  }
}

function formatRelativeTime(isoStr) {
  if (!isoStr) return "-";
  try {
    const diff = Math.floor((Date.now() - new Date(isoStr).getTime()) / 1000);
    if (diff < 5) return "just now";
    if (diff < 60) return `${diff}s ago`;
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    return `${Math.floor(diff / 86400)}d ago`;
  } catch {
    return isoStr;
  }
}

function truncate(str, maxLen) {
  if (!str) return "";
  return str.length > maxLen ? str.substring(0, maxLen - 1) + "…" : str;
}

// Initialize on DOM load
window.addEventListener("DOMContentLoaded", () => {
  window.app = new DashboardApp();
  window.app.init();
});

document.addEventListener("click", (e) => {
  let target = e.target.closest("[data-navigate], [data-app-change], [data-action], [data-cancel-wf], [data-resume-wf], [data-restart-wf], [data-wf-tab], [data-pause-schedule], [data-resume-schedule], [data-trigger-schedule], [data-delete-rule], [data-revoke-key], [data-copy-and-close], [data-json-mode]");
  if (!target) {
    if (e.target.hasAttribute("data-action") && e.target.getAttribute("data-action") === "closeModalOverlay") {
      window.app.closeModal();
    }
    // Check DAG node
    let dagNode = e.target.closest("[data-step-json]");
    if (dagNode) {
      window.selectStep(dagNode.getAttribute("data-step-json"));
    }
    // Check json viewer copy
    let copyBtn = e.target.closest("[data-copy]");
    if (copyBtn) {
      navigator.clipboard.writeText(copyBtn.getAttribute("data-copy")).then(() => {
        copyBtn.innerText = "Copied!";
        setTimeout(() => copyBtn.innerText = "Copy", 1500);
      });
    }
    return;
  }

  if (target.dataset.navigate) window.app.navigate(target.dataset.navigate.replace(/'/g, ''));
  else if (target.dataset.appChange) window.app.onAppChange(target.dataset.appChange.replace(/'/g, ''));
  else if (target.dataset.action === "refresh") window.app.renderContentView();
  else if (target.dataset.action === "toggleTheme") window.app.toggleTheme();
  else if (target.dataset.action === "closeModal") window.app.closeModal();
  else if (target.dataset.action === "closeMobileNav") window.app.closeMobileNav();
  else if (target.dataset.action === "toggleMobileNav") window.app.toggleMobileNav();
  else if (target.dataset.action === "executePendingConfirm") window.app.executePendingConfirm();
  else if (target.dataset.action === "openSignIn") window.app.openSignInModal();
  else if (target.dataset.action === "signOut") window.app.signOut();
  else if (target.dataset.action === "submitModalSignIn") {
    const input = document.getElementById("modal-auth-key-input");
    window.app.submitSignIn(input ? input.value.trim() : "");
  }
  else if (target.dataset.action === "submitSignIn") {
    const input = document.getElementById("auth-key-input");
    window.app.submitSignIn(input ? input.value.trim() : "");
  }
  else if (target.dataset.action === "viewChildWorkflow") {
    const childId = target.dataset.childWfId || "";
    if (window.app && window.app.selectedWorkflowId) {
      window.app.parentWorkflowMap.set(childId, {
        parentId: window.app.selectedWorkflowId,
        parentName: window.app.currentWorkflowName || window.app.selectedWorkflowId,
        parentStatus: window.app.currentWorkflowStatus || "SUCCESS"
      });
    }
    window.app.closeModal();
    window.app.navigate(`workflow/${encodeURIComponent(childId)}`);
  }
  else if (target.dataset.action === "expandPayload") {
    window.app.openExpandPayloadModal(target.dataset.payloadTitle, target.dataset.payloadRaw);
  }
  else if (target.dataset.action === "dagZoomIn") window.app.zoomDag(0.15);
  else if (target.dataset.action === "dagZoomOut") window.app.zoomDag(-0.15);
  else if (target.dataset.action === "dagReset") window.app.resetDagZoom();
  else if (target.dataset.jsonMode) {
    const mode = target.dataset.jsonMode;
    const targetId = target.dataset.targetId;
    const viewer = document.getElementById(targetId);
    if (viewer) {
      viewer.querySelectorAll(".json-segment-btn").forEach(b => b.classList.toggle("active", b.dataset.jsonMode === mode));
      const decodedEl = viewer.querySelector(".json-view-decoded");
      const rawEl = viewer.querySelector(".json-view-raw");
      if (decodedEl) decodedEl.style.display = mode === "decoded" ? "" : "none";
      if (rawEl) rawEl.style.display = mode === "raw" ? "" : "none";
    }
  }
  else if (target.dataset.cancelWf) window.app.cancelWorkflow(target.dataset.cancelWf.replace(/'/g, ''));
  else if (target.dataset.resumeWf) window.app.resumeWorkflow(target.dataset.resumeWf.replace(/'/g, ''));
  else if (target.dataset.restartWf) window.app.restartWorkflow(target.dataset.restartWf.replace(/'/g, ''));
  else if (target.dataset.wfTab) window.app.switchWfTab(target.dataset.wfTab.replace(/'/g, ''));
  else if (target.dataset.pauseSchedule) window.app.pauseSchedule(target.dataset.pauseSchedule.replace(/'/g, ''));
  else if (target.dataset.resumeSchedule) window.app.resumeSchedule(target.dataset.resumeSchedule.replace(/'/g, ''));
  else if (target.dataset.triggerSchedule) window.app.triggerSchedule(target.dataset.triggerSchedule.replace(/'/g, ''));
  else if (target.dataset.action === "openCreateAlert") window.app.openCreateAlertModal();
  else if (target.dataset.action === "submitCreateAlert") window.app.submitCreateAlert();
  else if (target.dataset.deleteRule) window.app.deleteAlertRule(target.dataset.deleteRule.replace(/'/g, ''));
  else if (target.dataset.action === "openCreateKey") window.app.openCreateKeyModal();
  else if (target.dataset.action === "submitCreateKey") window.app.submitCreateKey();
  else if (target.dataset.revokeKey) window.app.revokeKey(target.dataset.revokeKey.replace(/'/g, ''));
  else if (target.dataset.copyAndClose) {
    let text = target.dataset.copyAndClose;
    navigator.clipboard.writeText(text).then(() => {
      window.app.closeModal();
      window.app.showToast("API key copied to clipboard", "success");
      window.app.renderContentView();
    });
  }
});

document.addEventListener("change", (e) => {
  let target = e.target.closest("[data-change]");
  if (!target) return;
  if (target.dataset.change === "app") window.app.onAppChange(target.value);
  else if (target.dataset.change === "wfStatus") window.app.filterWorkflowsStatus(target.value);
  else if (target.dataset.change === "pollInterval") window.app.setPollInterval(target.value);
});

document.addEventListener("input", (e) => {
  let target = e.target.closest("[data-input]");
  if (!target) return;
  if (target.dataset.input === "wfTable") window.app.filterWorkflowsTable(target.value);
});

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    if (window.app && window.app.mobileNavOpen) {
      window.app.closeMobileNav();
      return;
    }
    const modal = document.getElementById("modal-root");
    if (modal && modal.children.length > 0) {
      window.app.closeModal();
      return;
    }
  }

  // Hotkey '/' to focus search input
  if (e.key === "/" && !e.ctrlKey && !e.metaKey) {
    const active = document.activeElement;
    if (active && (active.tagName === "INPUT" || active.tagName === "TEXTAREA" || active.tagName === "SELECT")) {
      return;
    }
    const filterInput = document.getElementById("filter-id") || document.querySelector("input[data-input]");
    if (filterInput) {
      e.preventDefault();
      filterInput.focus();
      filterInput.select();
      return;
    }
  }

  // Hotkey 'r' or 'R' to refresh content view
  if ((e.key === "r" || e.key === "R") && !e.ctrlKey && !e.metaKey) {
    const active = document.activeElement;
    if (active && (active.tagName === "INPUT" || active.tagName === "TEXTAREA" || active.tagName === "SELECT")) {
      return;
    }
    const modal = document.getElementById("modal-root");
    if (modal && modal.children.length > 0) return;
    if (window.app) {
      e.preventDefault();
      window.app.renderContentView();
      window.app.showToast("Telemetry refreshed", "info");
      return;
    }
  }

  // Keyboard activation for accessible table rows, dag nodes, and nav items
  if (e.key === "Enter" || e.key === " ") {
    const active = document.activeElement;
    if (!active) return;
    if (active.tagName === "INPUT" || active.tagName === "SELECT" || active.tagName === "TEXTAREA" || active.tagName === "BUTTON") {
      return;
    }
    if (active.classList && (active.classList.contains("clickable") || active.hasAttribute("data-step-json") || active.classList.contains("nav-item"))) {
      e.preventDefault();
      active.click();
      return;
    }
  }

  // Focus trap in active modal or drawer
  if (e.key === "Tab") {
    const dialog = document.querySelector("#modal-root .modal-dialog, #modal-root .drawer-panel");
    if (!dialog) return;

    const focusables = dialog.querySelectorAll('button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])');
    if (focusables.length === 0) return;

    const first = focusables[0];
    const last = focusables[focusables.length - 1];

    if (e.shiftKey) {
      if (document.activeElement === first) {
        e.preventDefault();
        last.focus();
      }
    } else {
      if (document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    }
  }
});
