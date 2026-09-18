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
    this.sseSource = null;
    this.mobileNavOpen = false;
    this.pendingConfirmAction = null;
    this.parentWorkflowMap = new Map();
    this.workflowAppMap = new Map();
    this.selectedWorkflowApp = "";
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

  isNoExecutorError(err) {
    if (!err) return false;
    if (err.status === 503) {
      const msg = (err.message || "").toLowerCase();
      if (msg.includes("executor") || msg.includes("unavailable")) return true;
    }
    const msg = (err.message || String(err)).toLowerCase();
    return msg.includes("no live executor") || msg.includes("no executors available");
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

  renderNoExecutorState(el, entityName = "queues") {
    const isQueue = entityName === "queues";
    const title = isQueue ? "Queues" : "Scheduled Jobs";
    const desc = isQueue
      ? `Queue configurations and worker concurrency limits are reported dynamically by active application executors. Once an executor for <strong>${escapeHtml(this.appName || "this application")}</strong> connects to Relay, its active queues will appear here.`
      : `Scheduled workflow jobs and cron triggers are discovered dynamically from connected application executors. Start an executor for <strong>${escapeHtml(this.appName || "this application")}</strong> to view and trigger scheduled workflows.`;

    el.innerHTML = `
      <div class="card">
        <div class="card-header">
          <span class="card-title">${title} (${escapeHtml(this.appName || "No app")})</span>
          <button class="btn btn-xs btn-secondary" data-action="refresh">Check Again</button>
        </div>
        <div class="card-body">
          <div class="empty-state">
            <div class="empty-state-icon icon-offline">
              ${isQueue ? `
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect>
                  <rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>
                  <line x1="6" y1="6" x2="6.01" y2="6"></line>
                  <line x1="6" y1="18" x2="6.01" y2="18"></line>
                </svg>
              ` : `
                <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <circle cx="12" cy="12" r="10"></circle>
                  <polyline points="12 6 12 12 16 14"></polyline>
                </svg>
              `}
            </div>
            <h4 class="empty-state-title">No Connected Executors</h4>
            <p class="empty-state-desc">${desc}</p>
            <div class="empty-state-hint">
              <div style="color:var(--text-tertiary); margin-bottom:4px;">Connect your application by configuring its Conductor URL:</div>
              <code>conductor_url: "ws://&lt;relay-host&gt;:8080"</code>
            </div>
            <div class="empty-state-actions">
              <button class="btn btn-sm btn-secondary" data-action="refresh">Check Again</button>
            </div>
          </div>
        </div>
      </div>
    `;
  }

  renderErrorState(el, title, message) {
    el.innerHTML = `
      <div class="card">
        <div class="card-header">
          <span class="card-title">${escapeHtml(title)}</span>
        </div>
        <div class="card-body">
          <div class="empty-state">
            <div class="empty-state-icon icon-error">
              <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <circle cx="12" cy="12" r="10"></circle>
                <line x1="12" y1="8" x2="12" y2="12"></line>
                <line x1="12" y1="16" x2="12.01" y2="16"></line>
              </svg>
            </div>
            <h4 class="empty-state-title">Unable to Load Data</h4>
            <p class="empty-state-desc">${escapeHtml(message)}</p>
            <div class="empty-state-actions">
              <button class="btn btn-sm btn-secondary" data-action="refresh">Retry</button>
            </div>
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

  setPollInterval(mode) {
    if (this.pollTimer) {
      clearInterval(this.pollTimer);
      this.pollTimer = null;
    }
    if (this.sseSource) {
      this.sseSource.close();
      this.sseSource = null;
    }

    const dot = document.querySelector(".poll-dot");

    if (mode === "stream") {
      this.pollInterval = "stream";
      this.startSSE();
      return;
    }

    this.pollInterval = parseInt(mode, 10) || 0;
    if (this.pollInterval > 0) {
      this.pollTimer = setInterval(() => this.poll(), this.pollInterval);
    }
    if (dot) {
      if (this.pollInterval > 0) {
        dot.classList.add("active");
        dot.classList.remove("error");
        dot.title = `Live polling active (${this.pollInterval / 1000}s)`;
      } else {
        dot.classList.remove("active");
        dot.classList.remove("error");
        dot.title = "Live telemetry refresh off";
      }
    }
  }

  startSSE() {
    if (this.sseSource) {
      this.sseSource.close();
      this.sseSource = null;
    }

    const dot = document.querySelector(".poll-dot");
    const url = this.client.getEventsUrl(this.orgName, this.appName);

    try {
      const sse = new EventSource(url);
      this.sseSource = sse;

      sse.onopen = () => {
        if (dot) {
          dot.classList.add("active");
          dot.classList.remove("error");
          dot.title = "Live streaming (SSE) connected";
        }
      };

      let debounceTimer = null;
      const scheduleRefresh = () => {
        if (debounceTimer) clearTimeout(debounceTimer);
        debounceTimer = setTimeout(() => {
          this.poll();
        }, 150);
      };

      sse.addEventListener("workflow_update", () => scheduleRefresh());
      sse.addEventListener("executor_change", () => scheduleRefresh());
      sse.addEventListener("ready", () => scheduleRefresh());

      sse.onerror = () => {
        if (dot) {
          dot.classList.remove("active");
          dot.classList.add("error");
          dot.title = "Streaming disconnected, attempting reconnect...";
        }
      };
    } catch (err) {
      console.error("Failed to establish SSE connection, falling back to polling:", err);
      this.setPollInterval("5000");
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
    const params = new URLSearchParams(window.location.search);
    const themeParam = params.get("theme");
    if (themeParam === "light" || themeParam === "dark") {
      this.theme = themeParam;
    }
    const appParam = params.get("app");
    if (appParam) {
      this.appName = appParam;
      localStorage.setItem("relay_selected_app", appParam);
    }
    this.applyTheme(this.theme);
    window.addEventListener("hashchange", () => this.handleRouting());

    await this.loadApplications();
    if (appParam && this.apps.some(a => a.name === appParam)) {
      this.appName = appParam;
    }
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
      const savedApp = localStorage.getItem("relay_selected_app");
      if (savedApp && this.apps.some(a => a.name === savedApp)) {
        this.appName = savedApp;
      } else {
        this.appName = "";
      }
    } catch (err) {
      console.warn("Listing applications:", err);
      this.apps = [];
      this.appName = "";
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
      { id: "keys", label: "API Keys", icon: `<circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6"/><path d="m15.5 7.5 3 3L22 7l-3-3"/>` },
    ];

    return `
      <aside class="sidebar ${this.mobileNavOpen ? 'mobile-open' : ''}">
        <div class="sidebar-header">
          <a href="#/fleet" class="brand-logo" aria-label="Relay Home">
            <svg width="24" height="24" viewBox="0 0 32 32" fill="none" aria-hidden="true">
              <path d="M9 6.5V25.5" stroke="var(--color-primary)" stroke-width="3" stroke-linecap="round"/>
              <path d="M9 7.5H17C20.5899 7.5 23.5 10.4101 23.5 14C23.5 17.5899 20.5899 20.5 17 20.5H9" stroke="var(--color-primary)" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/>
              <path d="M15.5 19.5L22.5 25.5" stroke="var(--color-purple)" stroke-width="3" stroke-linecap="round"/>
              <circle cx="23" cy="25" r="2" fill="var(--color-purple)"/>
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
              <option value="" ${!this.appName ? "selected" : ""}>All Applications</option>
              ${this.apps.map(a => `<option value="${escapeHtml(a.name)}" ${a.name === this.appName ? "selected" : ""}>${escapeHtml(a.name)}</option>`).join("")}
            </select>
          </div>
          <div class="selector-group">
            <span class="poll-indicator" title="Live telemetry refresh">
              <span class="poll-dot ${this.pollInterval === 'stream' || this.pollInterval > 0 ? 'active' : ''}"></span>
              <span>Live:</span>
            </span>
            <select class="select-sm" data-change="pollInterval" aria-label="Live telemetry mode">
              <option value="0" ${this.pollInterval === 0 ? "selected" : ""}>Off</option>
              <option value="stream" ${this.pollInterval === "stream" ? "selected" : ""}>Stream (SSE)</option>
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
      case "fleet": return this.appName ? `Fleet: ${this.appName}` : "Fleet & Applications";
      case "workflows": return this.appName ? `Workflows: ${this.appName}` : "Workflows";
      case "workflow-detail": return `Workflow: ${this.selectedWorkflowId || ""}`;
      case "queues": return this.appName ? `Queues: ${this.appName}` : "Queues";
      case "schedules": return this.appName ? `Schedules: ${this.appName}` : "Schedules";
      case "alerting": return this.appName ? `Alert Rules: ${this.appName}` : "Alerting Rules";
      case "keys": return "API Keys";
      default: return "Relay Dashboard";
    }
  }

  onAppChange(newAppName) {
    this.appName = newAppName || "";
    if (newAppName) {
      localStorage.setItem("relay_selected_app", newAppName);
    } else {
      localStorage.removeItem("relay_selected_app");
    }
    const selects = document.querySelectorAll('[data-change="app"]');
    selects.forEach(s => { s.value = this.appName; });
    const headerTitle = document.querySelector(".header-title");
    if (headerTitle) {
      headerTitle.textContent = this.getRouteTitle();
    }
    if (this.pollInterval === "stream") {
      this.startSSE();
    }
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
      const allApps = this.apps || [];
      const isFiltered = Boolean(this.appName);
      const appsToQuery = isFiltered
        ? allApps.filter(a => a.name === this.appName)
        : allApps;
      const effectiveApps = appsToQuery.length > 0 ? appsToQuery : allApps;
      const selectedApp = isFiltered ? allApps.find(a => a.name === this.appName) : null;

      // Query overview data for the apps in scope
      const perAppResults = await Promise.allSettled(
        effectiveApps.map(async (app) => {
          const [execRes, wfRes, qRes, schRes] = await Promise.allSettled([
            this.client.listExecutors(this.orgName, app.name),
            this.client.listWorkflows(this.orgName, app.name, { limit: 25 }),
            this.client.listQueues ? this.client.listQueues(this.orgName, app.name) : Promise.resolve([]),
            this.client.listSchedules ? this.client.listSchedules(this.orgName, app.name) : Promise.resolve([])
          ]);
          return {
            app,
            executors: execRes.status === "fulfilled" ? (execRes.value || []) : [],
            workflows: wfRes.status === "fulfilled" ? (wfRes.value || []) : [],
            queues: qRes.status === "fulfilled" ? (qRes.value || []) : [],
            schedules: schRes.status === "fulfilled" ? (schRes.value || []) : []
          };
        })
      );

      let allExecutors = [];
      let allWorkflows = [];
      let totalQueues = 0;
      let totalSchedules = 0;
      const appMetrics = new Map();

      for (const res of perAppResults) {
        if (res.status === "fulfilled") {
          const { app, executors, workflows, queues, schedules } = res.value;
          executors.forEach(e => allExecutors.push({ ...e, appName: app.name }));
          workflows.forEach(w => {
            this.workflowAppMap.set(w.workflowId, app.name);
            allWorkflows.push({ ...w, appName: app.name });
          });
          totalQueues += queues.length;
          totalSchedules += schedules.length;
          appMetrics.set(app.name, {
            executors,
            healthyExecutors: executors.filter(e => e.status === "HEALTHY").length,
            workflowsCount: workflows.length,
            queuesCount: queues.length,
            schedulesCount: schedules.length
          });
        }
      }

      const activeExecutors = allExecutors.filter(e => e.status === "HEALTHY").length;
      const disconnectedExecutors = allExecutors.filter(e => e.status === "DISCONNECTED").length;

      const inFlightWfs = allWorkflows.filter(w => w.status === "PENDING" || w.status === "ENQUEUED").length;
      const failedWfs = allWorkflows.filter(w => w.status === "ERROR").length;

      const recentWorkflows = [...allWorkflows]
        .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
        .slice(0, 8);

      const filterBannerHtml = isFiltered ? `
        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:16px; padding:10px 16px; background:var(--card-bg); border:1px solid var(--border-color); border-radius:var(--radius-md);">
          <div style="display:flex; align-items:center; gap:8px;">
            <span style="font-size:12px; color:var(--text-secondary);">Filtered to application:</span>
            <span class="badge badge-info">${escapeHtml(this.appName)}</span>
            ${selectedApp ? `<span class="badge badge-neutral">${escapeHtml(selectedApp.language || "dbos")}</span>` : ""}
            ${selectedApp ? renderStatusPill(selectedApp.status) : ""}
          </div>
          <button class="btn btn-xs btn-secondary" data-app-change="" aria-label="Reset filter to all applications">Show All Applications</button>
        </div>
      ` : "";

      const kpiCardsHtml = isFiltered ? `
        <div class="stat-grid">
          <div class="stat-card">
            <span class="stat-label">Application Status</span>
            <div style="margin-top:6px;">
              ${selectedApp ? renderStatusPill(selectedApp.status) : `<span class="badge badge-neutral">UNKNOWN</span>`}
            </div>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill">${selectedApp && selectedApp.privateMode ? "Private mode" : "Public mode"}</span>
              <span class="kpi-sub-pill">${selectedApp ? (selectedApp.executorTimeoutSecs || 60) : 60}s timeout</span>
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
            <span class="stat-label">Workflows</span>
            <span class="stat-value">${allWorkflows.length}</span>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill kpi-info">${inFlightWfs} in flight</span>
              <span class="kpi-sub-pill ${failedWfs > 0 ? 'kpi-error' : 'kpi-success'}">${failedWfs} errors</span>
            </div>
          </div>
          <div class="stat-card">
            <span class="stat-label">Active Queues & Schedules</span>
            <span class="stat-value">${totalQueues + totalSchedules}</span>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill">${totalQueues} queues</span>
              <span class="kpi-sub-pill">${totalSchedules} schedules</span>
            </div>
          </div>
        </div>
      ` : `
        <div class="stat-grid">
          <div class="stat-card">
            <span class="stat-label">Applications</span>
            <span class="stat-value">${allApps.length}</span>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill kpi-success">${allApps.filter(a => a.status === 'AVAILABLE').length} active</span>
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
            <span class="stat-label">Fleet Workflows</span>
            <span class="stat-value">${allWorkflows.length}</span>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill kpi-info">${inFlightWfs} in flight</span>
              <span class="kpi-sub-pill ${failedWfs > 0 ? 'kpi-error' : 'kpi-success'}">${failedWfs} errors</span>
            </div>
          </div>
          <div class="stat-card">
            <span class="stat-label">Active Queues & Schedules</span>
            <span class="stat-value">${totalQueues + totalSchedules}</span>
            <div class="kpi-subtext">
              <span class="kpi-sub-pill">${totalQueues} queues</span>
              <span class="kpi-sub-pill">${totalSchedules} schedules</span>
            </div>
          </div>
        </div>
      `;

      const appSectionHtml = isFiltered ? `
        <div class="card">
          <div class="card-header">
            <div>
              <span class="card-title">Application Details: ${escapeHtml(this.appName)}</span>
              <span style="font-size:12px; color:var(--text-secondary); margin-left:8px;">Runtime configuration</span>
            </div>
            <div style="display:flex; gap:6px;">
              <button class="btn btn-xs btn-primary" data-app-change='${escapeHtml(this.appName)}' data-navigate="workflows">Workflows →</button>
              <button class="btn btn-xs btn-secondary" data-app-change='${escapeHtml(this.appName)}' data-navigate="queues">Queues</button>
            </div>
          </div>
          <div class="card-body">
            <div class="stat-grid" style="grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); margin-bottom:0;">
              <div>
                <span class="stat-label">Application Name</span>
                <div><strong>${escapeHtml(this.appName)}</strong></div>
              </div>
              <div>
                <span class="stat-label">Runtime Language</span>
                <div><span class="badge badge-neutral">${escapeHtml(selectedApp ? (selectedApp.language || "dbos") : "-")}</span></div>
              </div>
              <div>
                <span class="stat-label">Executor Timeout</span>
                <div>${selectedApp ? (selectedApp.executorTimeoutSecs || 60) : 60} seconds</div>
              </div>
              <div>
                <span class="stat-label">Network Mode</span>
                <div>${selectedApp && selectedApp.privateMode ? "Private network" : "Public access"}</div>
              </div>
              <div>
                <span class="stat-label">Live Executors</span>
                <div>
                  <span class="badge ${activeExecutors > 0 ? 'badge-success' : 'badge-neutral'}">
                    ${activeExecutors} live
                  </span>
                </div>
              </div>
              <div>
                <span class="stat-label">Data Access Mode</span>
                <div>
                  <span class="badge ${activeExecutors > 0 ? 'badge-success' : 'badge-warning'}" title="${activeExecutors > 0 ? 'Operational telemetry accessible via active executor runtime' : 'No live executors connected for application database queries'}">
                    ${activeExecutors > 0 ? `Executor Hub (${activeExecutors} active)` : 'No Live Executors'}
                  </span>
                </div>
              </div>
            </div>
            <div style="margin-top:16px; padding:12px; background:var(--bg-tertiary); border:1px solid var(--border-color); border-radius:var(--radius-sm); font-size:12px; color:var(--text-secondary); display:flex; align-items:flex-start; gap:10px;">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink:0; margin-top:2px; color:var(--color-info-text);" aria-hidden="true">
                <ellipse cx="12" cy="5" rx="9" ry="3"></ellipse>
                <path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"></path>
                <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"></path>
              </svg>
              <div>
                <div style="color:var(--text-primary); font-weight:600; margin-bottom:2px;">Application Data Plane</div>
                <div>
                  ${activeExecutors > 0
                    ? `Operational telemetry and workflow dispatches are serviced via ${activeExecutors} connected ${activeExecutors === 1 ? 'executor' : 'executors'} connecting to the application system database. Relay coordinates executions without storing database credentials.`
                    : `No executors are currently connected. Live dispatch and queue operations require an active executor connected to the application database (or direct data plane fallback configured in Relay server).`
                  }
                </div>
              </div>
            </div>
          </div>
        </div>
      ` : `
        <div class="card">
          <div class="card-header">
            <div>
              <span class="card-title">Applications</span>
              <span style="font-size:12px; color:var(--text-secondary); margin-left:8px;">Registered applications in fleet</span>
            </div>
            <span class="badge badge-neutral">${allApps.length} total</span>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Application</th>
                  <th>Status</th>
                  <th>Runtime</th>
                  <th>Live Executors</th>
                  <th>Data Access</th>
                  <th>Workflows</th>
                  <th>Queues / Schedules</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                ${allApps.length > 0 ? allApps.map(a => {
                  const m = appMetrics.get(a.name) || { healthyExecutors: 0, workflowsCount: 0, queuesCount: 0, schedulesCount: 0 };
                  return `
                    <tr>
                      <td>
                        <a href="#/fleet" class="clickable-app-name" data-app-change='${escapeHtml(a.name)}' style="font-weight:600; text-decoration:none; color:inherit;" title="Inspect ${escapeHtml(a.name)} details on Fleet view">
                          ${escapeHtml(a.name)}
                        </a>
                      </td>
                      <td>${renderStatusPill(a.status)}</td>
                      <td><span class="badge badge-neutral">${escapeHtml(a.language || "dbos")}</span></td>
                      <td>
                        <span class="badge ${m.healthyExecutors > 0 ? 'badge-success' : 'badge-neutral'}">
                          ${m.healthyExecutors} live
                        </span>
                      </td>
                      <td>
                        <span class="badge ${m.healthyExecutors > 0 ? 'badge-success' : 'badge-warning'}" style="font-size:11px;" title="${m.healthyExecutors > 0 ? 'Data plane active via ' + m.healthyExecutors + ' connected executor(s)' : 'No live executors connected'}">
                          ${m.healthyExecutors > 0 ? 'Executor Hub' : 'No Executors'}
                        </span>
                      </td>
                      <td>${m.workflowsCount} recorded</td>
                      <td>${m.queuesCount}q / ${m.schedulesCount}s</td>
                      <td>
                        <div style="display:flex; gap:6px;">
                          <button class="btn btn-xs btn-secondary" data-app-change='${escapeHtml(a.name)}' title="Inspect application details on Fleet view">Details</button>
                          <button class="btn btn-xs btn-primary" data-app-change='${escapeHtml(a.name)}' data-navigate="workflows">Workflows →</button>
                        </div>
                      </td>
                    </tr>
                  `;
                }).join("") : `<tr><td colspan="8" style="text-align:center; color:var(--text-tertiary); padding:24px;">No applications registered.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;

      el.innerHTML = `
        ${filterBannerHtml}
        ${kpiCardsHtml}
        ${appSectionHtml}

        <div class="card">
          <div class="card-header">
            <div>
              <span class="card-title">${isFiltered ? `Recent Workflows (${escapeHtml(this.appName)})` : "Recent Fleet Workflows"}</span>
              <span style="font-size:12px; color:var(--text-secondary); margin-left:8px;">
                ${isFiltered ? `Live operational telemetry for ${escapeHtml(this.appName)}` : "Live operational telemetry across applications"}
              </span>
            </div>
            <a href="#/workflows" class="btn btn-xs btn-secondary" data-navigate="workflows">
              ${isFiltered ? "View App Workflows →" : "View All Workflows →"}
            </a>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Status</th>
                  ${!isFiltered ? "<th>Application</th>" : ""}
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
                  <tr class="clickable" tabindex="0" role="button" aria-label="View workflow ${escapeHtml(w.workflowId)}" data-navigate="workflow/${escapeHtml(w.workflowId)}" data-wf-app="${escapeHtml(w.appName)}">
                    <td>${renderStatusPill(w.status)}</td>
                    ${!isFiltered ? `<td><span class="badge badge-info">${escapeHtml(w.appName)}</span></td>` : ""}
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
                      <a href="#/workflow/${escapeHtml(w.workflowId)}" class="btn btn-xs btn-secondary" data-navigate="workflow/${escapeHtml(w.workflowId)}" data-wf-app="${escapeHtml(w.appName)}">Inspect ↗</a>
                    </td>
                  </tr>
                `).join("") : `<tr><td colspan="${isFiltered ? '7' : '8'}" style="text-align:center; color:var(--text-tertiary); padding:24px;">No workflow executions recorded${isFiltered ? ` for ${escapeHtml(this.appName)}` : " across applications"}.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <div>
              <span class="card-title">${isFiltered ? `Connected Executors (${escapeHtml(this.appName)})` : "Connected Executors"}</span>
              <span style="font-size:12px; color:var(--text-secondary); margin-left:8px;">
                ${isFiltered ? `Live nodes connected for ${escapeHtml(this.appName)}` : "Live nodes connected to Relay"}
              </span>
            </div>
            <span class="badge badge-neutral">${allExecutors.length} total</span>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Executor ID</th>
                  ${!isFiltered ? "<th>Application</th>" : ""}
                  <th>Status</th>
                  <th>Hostname</th>
                  <th>Version</th>
                  <th>Language</th>
                  <th>Last Seen</th>
                </tr>
              </thead>
              <tbody>
                ${allExecutors.length > 0 ? allExecutors.map(e => `
                  <tr>
                    <td><code>${escapeHtml(e.executorId)}</code></td>
                    ${!isFiltered ? `<td><span class="badge badge-info">${escapeHtml(e.appName)}</span></td>` : ""}
                    <td>${renderStatusPill(e.status)}</td>
                    <td>${escapeHtml(e.hostname || "localhost")}</td>
                    <td><span class="badge">${escapeHtml(e.appVersion || "v1.0.0")}</span></td>
                    <td>${escapeHtml(e.language || "unknown")}</td>
                    <td>${formatTimestamp(e.updatedAt)}</td>
                  </tr>
                `).join("") : `<tr><td colspan="${isFiltered ? '6' : '7'}" style="text-align:center; color:var(--text-tertiary); padding:24px;">No executors currently connected${isFiltered ? ` for ${escapeHtml(this.appName)}` : ""}.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      this.renderErrorState(el, this.appName ? `Fleet (${escapeHtml(this.appName)})` : "Fleet & Applications", err.message);
    }
  }

  // --- SCREEN 2: WORKFLOW SEARCH & LIST ---
  async renderWorkflowsScreen(el, silent = false) {
    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading workflows...</div>`;
    }

    try {
      let workflows = [];
      if (!this.appName) {
        const results = await Promise.allSettled(
          (this.apps || []).map(async (app) => {
            const list = await this.client.listWorkflows(this.orgName, app.name, { limit: 50 });
            return list.map(w => ({ ...w, appName: app.name }));
          })
        );
        for (const r of results) {
          if (r.status === "fulfilled") {
            workflows.push(...r.value);
          }
        }
        workflows.sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime());
      } else {
        const list = await this.client.listWorkflows(this.orgName, this.appName, { limit: 50 });
        workflows = list.map(w => ({ ...w, appName: this.appName }));
      }

      // Record workflow application mapping for fast detail resolution
      workflows.forEach(w => {
        if (w.appName) {
          this.workflowAppMap.set(w.workflowId, w.appName);
        }
      });

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
            <span class="text-secondary" style="font-size:12px;">Showing ${workflows.length} workflows${this.appName ? ` (${escapeHtml(this.appName)})` : ""}</span>
          </div>
        </div>

        <div class="card">
          <div class="table-container">
            <table class="data-table" id="workflows-table">
              <thead>
                <tr>
                  <th>Workflow ID</th>
                  <th>Application</th>
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
                  <tr class="clickable" tabindex="0" role="button" aria-label="View workflow ${escapeHtml(wf.workflowId)}" data-navigate='workflow/${encodeURIComponent(wf.workflowId)}' data-wf-app="${escapeHtml(wf.appName || '')}">
                    <td><code>${escapeHtml(wf.workflowId)}</code></td>
                    <td><span class="badge badge-info">${escapeHtml(wf.appName || 'default')}</span></td>
                    <td>${renderStatusPill(wf.status)}</td>
                    <td><strong>${escapeHtml(wf.workflowName || "unnamed")}</strong></td>
                    <td>${escapeHtml(wf.queueName || "default")}</td>
                    <td>${escapeHtml(wf.appVersion || "-")}</td>
                    <td>${formatTimestamp(wf.createdAt)}</td>
                    <td>${calculateDuration(wf.createdAt, wf.completedAt)}</td>
                  </tr>
                `).join("") : `<tr><td colspan="8" style="text-align:center; color:var(--text-tertiary); padding:24px;">No workflows found.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      this.renderErrorState(el, `Workflows (${escapeHtml(this.appName || 'All Applications')})`, err.message);
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
      let targetApp = this.selectedWorkflowApp || this.workflowAppMap.get(this.selectedWorkflowId) || this.appName;
      if (!targetApp && this.apps && this.apps.length > 0) {
        for (const app of this.apps) {
          try {
            await this.client.getWorkflow(this.orgName, app.name, this.selectedWorkflowId);
            targetApp = app.name;
            this.workflowAppMap.set(this.selectedWorkflowId, targetApp);
            break;
          } catch {
            // continue search
          }
        }
      }
      if (!targetApp) {
        targetApp = this.appName || (this.apps[0] ? this.apps[0].name : "default");
      }
      this.selectedWorkflowApp = targetApp;

      const wf = await this.client.getWorkflow(this.orgName, targetApp, this.selectedWorkflowId);
      const steps = await this.client.listSteps(this.orgName, targetApp, this.selectedWorkflowId);

      this.currentWorkflowName = wf.workflowName || wf.workflowId;
      this.currentWorkflowStatus = wf.status;

      // Check for child workflows spawned by any step
      const childStepEntries = steps.filter(s => Boolean(s.childWorkflowId));
      let childWorkflows = [];

      if (childStepEntries.length > 0) {
        const childResults = await Promise.allSettled(
          childStepEntries.map(async (step) => {
            try {
              const childWf = await this.client.getWorkflow(this.orgName, targetApp, step.childWorkflowId);
              const childSteps = await this.client.listSteps(this.orgName, targetApp, step.childWorkflowId);
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
          <span class="badge badge-info" style="font-size:11px;">${escapeHtml(targetApp)}</span>
          <span class="wf-breadcrumb-separator" aria-hidden="true">/</span>
          ${parentInfo ? `
            <a href="#/workflow/${encodeURIComponent(parentInfo.parentId)}" class="wf-breadcrumb-link" data-navigate="workflow/${encodeURIComponent(parentInfo.parentId)}" data-wf-app="${escapeHtml(targetApp)}" aria-label="Parent workflow ${escapeHtml(parentInfo.parentName)}">
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
                <button class="btn btn-sm btn-danger" data-cancel-wf='${escapeHtml(wf.workflowId)}' data-target-app='${escapeHtml(targetApp)}'>Cancel</button>
              ` : ""}
              ${wf.status === "CANCELLED" ? `
                <button class="btn btn-sm btn-primary" data-resume-wf='${escapeHtml(wf.workflowId)}' data-target-app='${escapeHtml(targetApp)}'>Resume</button>
              ` : ""}
              ${wf.status === "ERROR" ? `
                <button class="btn btn-sm btn-primary" data-restart-wf='${escapeHtml(wf.workflowId)}' data-target-app='${escapeHtml(targetApp)}'>Restart</button>
              ` : ""}
            </div>
          </div>
          <div class="card-body">
            <div class="stat-grid" style="grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); margin-bottom: 0;">
              <div>
                <span class="stat-label">Application</span>
                <div><span class="badge badge-info">${escapeHtml(targetApp)}</span></div>
              </div>
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
      this.renderErrorState(el, `Workflow Details (${escapeHtml(this.selectedWorkflowId || 'Unknown')})`, err.message);
    }
  }

  async switchWfTab(tab) {
    document.querySelectorAll(".tab-btn").forEach(b => b.classList.remove("active"));
    const btn = document.getElementById(`tab-btn-${tab}`);
    if (btn) btn.classList.add("active");

    const content = document.getElementById("wf-tab-content");
    if (!content) return;

    const targetApp = this.selectedWorkflowApp || this.workflowAppMap.get(this.selectedWorkflowId) || this.appName;

    if (tab === "io") {
      const wf = await this.client.getWorkflow(this.orgName, targetApp, this.selectedWorkflowId);
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
      const events = await this.client.getWorkflowEvents(this.orgName, targetApp, this.selectedWorkflowId);
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
      const notifs = await this.client.getWorkflowNotifications(this.orgName, targetApp, this.selectedWorkflowId);
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

  async cancelWorkflow(id, appName) {
    const targetApp = appName || this.selectedWorkflowApp || this.workflowAppMap.get(id) || this.appName;
    this.showConfirm({
      title: "Cancel Workflow Execution",
      message: `Are you sure you want to cancel workflow ${id}?`,
      consequence: "Execution will halt immediately. In-flight and pending steps will be cancelled and cannot be resumed without explicit operator intervention.",
      details: [
        { label: "Workflow ID", value: id },
        { label: "Application", value: targetApp || "default" },
        { label: "Transition", value: "-> CANCELLED" }
      ],
      confirmText: "Cancel Workflow",
      confirmClass: "btn-danger",
      onConfirm: async () => {
        try {
          await this.client.cancelWorkflow(this.orgName, targetApp, id);
          this.showToast(`Workflow ${id} cancelled`, "success");
          this.renderWorkflowDetailScreen(document.getElementById("content-view"));
        } catch (err) {
          this.showToast(`Failed to cancel: ${err.message}`, "error");
        }
      }
    });
  }

  async resumeWorkflow(id, appName) {
    const targetApp = appName || this.selectedWorkflowApp || this.workflowAppMap.get(id) || this.appName;
    try {
      await this.client.resumeWorkflow(this.orgName, targetApp, id);
      this.showToast(`Resumed workflow ${id}`, "success");
      this.renderWorkflowDetailScreen(document.getElementById("content-view"));
    } catch (err) {
      this.showToast(`Failed to resume: ${err.message}`, "error");
    }
  }

  async restartWorkflow(id, appName) {
    const targetApp = appName || this.selectedWorkflowApp || this.workflowAppMap.get(id) || this.appName;
    try {
      const res = await this.client.forkWorkflow(this.orgName, targetApp, id, 0);
      this.showToast(`Restarted workflow! New ID: ${res.workflowId}`, "success");
      if (res.workflowId && targetApp) {
        this.selectedWorkflowApp = targetApp;
        this.workflowAppMap.set(res.workflowId, targetApp);
      }
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
      let queues = [];
      if (!this.appName) {
        const results = await Promise.allSettled(
          (this.apps || []).map(async (app) => {
            try {
              const list = await this.client.listQueues(this.orgName, app.name);
              return (list || []).map(q => ({ ...q, appName: app.name }));
            } catch {
              return [];
            }
          })
        );
        for (const r of results) {
          if (r.status === "fulfilled") {
            queues.push(...r.value);
          }
        }
      } else {
        const list = await this.client.listQueues(this.orgName, this.appName);
        queues = (list || []).map(q => ({ ...q, appName: this.appName }));
      }

      el.innerHTML = `
        <div class="card">
          <div class="card-header">
            <div>
              <span class="card-title">Queues</span>
              <span style="font-size:12px; color:var(--text-secondary); margin-left:8px;">${this.appName ? escapeHtml(this.appName) : 'Across all applications'}</span>
            </div>
            <div style="display:flex; align-items:center; gap:8px;">
              <span class="badge badge-neutral">${queues.length} total</span>
              <button class="btn btn-xs btn-secondary" data-action="refresh">Refresh</button>
            </div>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Queue Name</th>
                  <th>Application</th>
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
                    <td><span class="badge badge-info">${escapeHtml(q.appName || 'default')}</span></td>
                    <td>${q.concurrency !== null ? q.concurrency : "Unlimited"}</td>
                    <td>${q.workerConcurrency !== null ? q.workerConcurrency : "-"}</td>
                    <td>${q.rateLimitMax ? `${q.rateLimitMax} / ${q.rateLimitPeriodSecs}s` : "None"}</td>
                    <td>${q.priorityEnabled ? "Yes" : "No"}</td>
                    <td>${q.partitionQueue ? "Yes" : "No"}</td>
                  </tr>
                `).join("") : `<tr><td colspan="7" style="text-align:center; color:var(--text-tertiary); padding:24px;">No queues configured.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      if (this.isNoExecutorError(err)) {
        this.renderNoExecutorState(el, "queues");
        return;
      }
      this.renderErrorState(el, `Queues (${escapeHtml(this.appName || 'All Applications')})`, err.message);
    }
  }

  // --- SCREEN 5: SCHEDULES ---
  async renderSchedulesScreen(el, silent = false) {
    if (!silent) {
      el.innerHTML = `<div class="loading-spinner">Loading schedules...</div>`;
    }

    try {
      let schedules = [];
      if (!this.appName) {
        const results = await Promise.allSettled(
          (this.apps || []).map(async (app) => {
            try {
              const list = await this.client.listSchedules(this.orgName, app.name);
              return (list || []).map(s => ({ ...s, appName: app.name }));
            } catch {
              return [];
            }
          })
        );
        for (const r of results) {
          if (r.status === "fulfilled") {
            schedules.push(...r.value);
          }
        }
      } else {
        const list = await this.client.listSchedules(this.orgName, this.appName);
        schedules = (list || []).map(s => ({ ...s, appName: this.appName }));
      }

      el.innerHTML = `
        <div class="card">
          <div class="card-header">
            <div>
              <span class="card-title">Scheduled Jobs</span>
              <span style="font-size:12px; color:var(--text-secondary); margin-left:8px;">${this.appName ? escapeHtml(this.appName) : 'Across all applications'}</span>
            </div>
            <div style="display:flex; align-items:center; gap:8px;">
              <span class="badge badge-neutral">${schedules.length} total</span>
              <button class="btn btn-xs btn-secondary" data-action="refresh">Refresh</button>
            </div>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Schedule Name</th>
                  <th>Application</th>
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
                    <td><span class="badge badge-info">${escapeHtml(s.appName || 'default')}</span></td>
                    <td>${escapeHtml(s.workflowName)}</td>
                    <td><code>${escapeHtml(s.cronExpression)}</code></td>
                    <td>${renderStatusPill(s.status)}</td>
                    <td>${formatTimestamp(s.lastFiredAt)}</td>
                    <td>
                      <div style="display:flex; gap:4px;">
                        ${s.status === "ACTIVE" ? `
                          <button class="btn btn-xs btn-secondary" data-pause-schedule='${escapeHtml(s.scheduleName)}' data-schedule-app='${escapeHtml(s.appName || '')}'>Pause</button>
                        ` : `
                          <button class="btn btn-xs btn-secondary" data-resume-schedule='${escapeHtml(s.scheduleName)}' data-schedule-app='${escapeHtml(s.appName || '')}'>Resume</button>
                        `}
                        <button class="btn btn-xs btn-primary" data-trigger-schedule='${escapeHtml(s.scheduleName)}' data-schedule-app='${escapeHtml(s.appName || '')}'>Trigger Now</button>
                      </div>
                    </td>
                  </tr>
                `).join("") : `<tr><td colspan="7" style="text-align:center; color:var(--text-tertiary); padding:24px;">No schedules configured.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      if (this.isNoExecutorError(err)) {
        this.renderNoExecutorState(el, "schedules");
        return;
      }
      this.renderErrorState(el, `Scheduled Jobs (${escapeHtml(this.appName || 'All Applications')})`, err.message);
    }
  }

  async pauseSchedule(name, appName) {
    const targetApp = appName || this.appName;
    try {
      await this.client.pauseSchedule(this.orgName, targetApp, name);
      this.showToast(`Schedule "${name}" paused`, "success");
      this.renderContentView();
    } catch (err) {
      this.showToast(`Failed to pause schedule: ${err.message}`, "error");
    }
  }

  async resumeSchedule(name, appName) {
    const targetApp = appName || this.appName;
    try {
      await this.client.resumeSchedule(this.orgName, targetApp, name);
      this.showToast(`Schedule "${name}" resumed`, "success");
      this.renderContentView();
    } catch (err) {
      this.showToast(`Failed to resume schedule: ${err.message}`, "error");
    }
  }

  async triggerSchedule(name, appName) {
    const targetApp = appName || this.appName;
    try {
      const res = await this.client.triggerSchedule(this.orgName, targetApp, name);
      this.showToast(`Triggered schedule "${name}". New Workflow: ${res.workflowId}`, "success");
      if (res.workflowId && targetApp) {
        this.selectedWorkflowApp = targetApp;
        this.workflowAppMap.set(res.workflowId, targetApp);
      }
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
      let rules = [];
      if (!this.appName) {
        const results = await Promise.allSettled(
          (this.apps || []).map(async (app) => {
            try {
              const list = await this.client.listAlertingRules(this.orgName, app.name);
              return (list || []).map(r => ({ ...r, appName: app.name }));
            } catch {
              return [];
            }
          })
        );
        for (const r of results) {
          if (r.status === "fulfilled") {
            rules.push(...r.value);
          }
        }
      } else {
        const list = await this.client.listAlertingRules(this.orgName, this.appName);
        rules = (list || []).map(r => ({ ...r, appName: this.appName }));
      }

      el.innerHTML = `
        <div class="toolbar">
          <div class="filter-group">
            <span class="text-secondary" style="font-size:12px;">Active Alert Rules${this.appName ? ` (${escapeHtml(this.appName)})` : ' across all applications'}</span>
          </div>
          <div>
            <button class="btn btn-sm btn-primary" data-action="openCreateAlert">+ New Alert Rule</button>
          </div>
        </div>

        <div class="card">
          <div class="card-header">
            <div>
              <span class="card-title">Active Alert Rules</span>
              <span style="font-size:12px; color:var(--text-secondary); margin-left:8px;">${this.appName ? escapeHtml(this.appName) : 'Across all applications'}</span>
            </div>
            <span class="badge badge-neutral">${rules.length} total</span>
          </div>
          <div class="table-container">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Rule ID</th>
                  <th>Application</th>
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
                    <td><span class="badge badge-info">${escapeHtml(r.appName || 'default')}</span></td>
                    <td><strong>${escapeHtml(r.ruleType)}</strong></td>
                    <td>${r.minIntervalSecs || 0}s</td>
                    <td><code>${escapeHtml(JSON.stringify(r.ruleMetadata || {}))}</code></td>
                    <td>${formatTimestamp(r.lastFiredAt)}</td>
                    <td>
                      <button class="btn btn-xs btn-danger" data-delete-rule='${escapeHtml(r.id)}' data-rule-app='${escapeHtml(r.appName || '')}'>Delete</button>
                    </td>
                  </tr>
                `).join("") : `<tr><td colspan="7" style="text-align:center; color:var(--text-tertiary); padding:24px;">No alerting rules configured.</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
      `;
    } catch (err) {
      if (this.isAuthError(err)) {
        this.renderAuthRequired(el, "load alert rules");
        return;
      }
      this.renderErrorState(el, `Alert Rules (${escapeHtml(this.appName || 'All Applications')})`, err.message);
    }
  }

  openCreateAlertModal() {
    const root = document.getElementById("modal-root");
    if (!root) return;

    const appOptions = (this.apps || []).map(a => `<option value="${escapeHtml(a.name)}" ${a.name === this.appName ? "selected" : ""}>${escapeHtml(a.name)}</option>`).join("");

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
              <label class="form-label" for="new-rule-app">Application</label>
              <select id="new-rule-app" class="select-sm">
                ${appOptions}
              </select>
            </div>
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
    const appSelect = document.getElementById("new-rule-app");
    if (appSelect) appSelect.focus();
  }

  async submitCreateAlert() {
    const appSelect = document.getElementById("new-rule-app");
    const targetApp = appSelect ? appSelect.value : (this.appName || (this.apps[0] ? this.apps[0].name : ""));
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
      await this.client.createAlertingRule(this.orgName, targetApp, {
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

  async deleteAlertRule(ruleId, appName) {
    const targetApp = appName || this.appName || (this.apps[0] ? this.apps[0].name : "default");
    this.showConfirm({
      title: "Delete Alert Rule",
      message: `Are you sure you want to delete alert rule "${ruleId}"?`,
      consequence: "Alert notifications defined by this rule will immediately stop firing. This action cannot be undone.",
      details: [
        { label: "Rule ID", value: ruleId },
        { label: "Application", value: targetApp },
        { label: "Action", value: "Permanent deletion" }
      ],
      confirmText: "Delete Rule",
      confirmClass: "btn-danger",
      onConfirm: async () => {
        try {
          await this.client.deleteAlertingRule(this.orgName, targetApp, ruleId);
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
      this.renderErrorState(el, "API Keys", err.message);
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

  if (target.dataset.navigate) {
    const navPath = target.dataset.navigate.replace(/'/g, '');
    const wfApp = target.dataset.wfApp || (target.closest("[data-wf-app]") ? target.closest("[data-wf-app]").dataset.wfApp : "");
    if (wfApp) {
      window.app.selectedWorkflowApp = wfApp;
      const parts = navPath.split("/");
      if (parts[0] === "workflow" && parts[1]) {
        window.app.workflowAppMap.set(decodeURIComponent(parts[1]), wfApp);
      }
    }
    window.app.navigate(navPath);
  }
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
  else if (target.dataset.cancelWf) window.app.cancelWorkflow(target.dataset.cancelWf.replace(/'/g, ''), target.dataset.targetApp);
  else if (target.dataset.resumeWf) window.app.resumeWorkflow(target.dataset.resumeWf.replace(/'/g, ''), target.dataset.targetApp);
  else if (target.dataset.restartWf) window.app.restartWorkflow(target.dataset.restartWf.replace(/'/g, ''), target.dataset.targetApp);
  else if (target.dataset.wfTab) window.app.switchWfTab(target.dataset.wfTab.replace(/'/g, ''));
  else if (target.dataset.pauseSchedule) window.app.pauseSchedule(target.dataset.pauseSchedule.replace(/'/g, ''), target.dataset.scheduleApp);
  else if (target.dataset.resumeSchedule) window.app.resumeSchedule(target.dataset.resumeSchedule.replace(/'/g, ''), target.dataset.scheduleApp);
  else if (target.dataset.triggerSchedule) window.app.triggerSchedule(target.dataset.triggerSchedule.replace(/'/g, ''), target.dataset.scheduleApp);
  else if (target.dataset.action === "openCreateAlert") window.app.openCreateAlertModal();
  else if (target.dataset.action === "submitCreateAlert") window.app.submitCreateAlert();
  else if (target.dataset.deleteRule) window.app.deleteAlertRule(target.dataset.deleteRule.replace(/'/g, ''), target.dataset.ruleApp);
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
