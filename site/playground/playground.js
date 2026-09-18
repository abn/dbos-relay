// site/playground/playground.js - Interactive In-Browser Playground Controller

const channel = new BroadcastChannel("relay-playground-bus");

const DEMO_SCRIPTS = {
  checkout: `// DBOS Transact E-Commerce Checkout Workflow
// All steps and child workflows are persisted to in-browser PGlite (Postgres 16 WASM)

import { DBOS } from "@dbos-inc/dbos-sdk";

export class OrderService {
  @DBOS.workflow()
  static async processOrder(orderId: string, amount: number) {
    // Step 1: Validate customer cart items & prices
    const cart = await DBOS.runStep("validateCart", () => ({
      orderId,
      valid: true,
      items: ["dbos-pro-license", "relay-support-tier"]
    }));

    // Step 2: Reserve warehouse inventory
    await DBOS.runStep("reserveInventory", () => {
      return { reserved: true, sku: "DBOS-RELAY-KEY", qty: 2 };
    });

    // Step 3: Authorize payment gateway (Child Workflow)
    const payment = await DBOS.startWorkflow(PaymentService.authorizePayment, orderId, amount);
    await payment.getResult();

    // Step 4: Schedule carrier shipping & tracking
    await DBOS.runStep("scheduleShipping", () => ({
      carrier: "FastTrack",
      trackingNumber: "FT-" + Math.floor(Math.random() * 1000000)
    }));

    // Step 5: Send transactional email receipt
    await DBOS.runStep("sendReceiptNotification", () => ({
      delivered: true,
      email: "operator@example.com"
    }));

    return { status: "ORDER_FULFILLED", orderId };
  }
}`,

  payment: `// DBOS Transact Payment Processing with Idempotency & Retries
import { DBOS } from "@dbos-inc/dbos-sdk";

export class PaymentService {
  @DBOS.workflow()
  static async processPayment(paymentId: string, amount: number) {
    // Step 1: Verify token idempotency key in database
    await DBOS.runStep("checkIdempotencyKey", () => ({
      key: paymentId,
      status: "NEW"
    }));

    // Step 2: Risk and fraud velocity scoring
    const risk = await DBOS.runStep("riskScoring", () => ({
      score: 12,
      riskLevel: "LOW"
    }));

    // Step 3: Card network authorization with automatic exponential backoff
    await DBOS.runStep("authorizeCard", () => ({
      authCode: "AUTH-" + Math.floor(Math.random() * 900000 + 100000),
      amountCharged: amount
    }));

    return { paymentId, status: "SETTLED" };
  }
}`
};

// Global PGlite state accessible to iframe
window.pgliteDb = null;

class PlaygroundController {
  constructor() {
    this.injectFailure = false;
    this.isRunning = false;
    this.activeWorkflowId = null;
    this.initElements();
    this.initDatabase();
  }

  initElements() {
    this.runBtn = document.getElementById("run-btn");
    this.failBtn = document.getElementById("fail-toggle-btn");
    this.resetBtn = document.getElementById("reset-btn");
    this.themeBtn = document.getElementById("theme-btn");
    this.demoSelect = document.getElementById("demo-select");
    this.codeEditor = document.getElementById("code-editor");
    this.codeHighlighting = document.getElementById("code-highlighting");
    this.codeHighlightingContent = document.getElementById("code-highlighting-content");
    this.consoleLogs = document.getElementById("console-logs");
    this.sqlInput = document.getElementById("sql-input");
    this.sqlRunBtn = document.getElementById("sql-run-btn");
    this.queryResults = document.getElementById("query-results");
    this.dbStatus = document.getElementById("db-status");
    this.dbStatusText = document.getElementById("db-status-text");
    this.statusDot = this.dbStatus ? this.dbStatus.querySelector(".status-dot") : null;
    this.toggleEditorBtn = document.getElementById("toggle-editor-btn");
    this.leftPane = document.querySelector(".left-pane");
    this.editorFolded = localStorage.getItem("relay-playground-editor-folded") === "true";
    const urlParams = new URLSearchParams(window.location.search);
    if (urlParams.get("editor") === "folded") {
      this.editorFolded = true;
    } else if (urlParams.get("editor") === "expanded") {
      this.editorFolded = false;
    }
    if (this.editorFolded) this.applyEditorFold(true);

    if (this.toggleEditorBtn) {
      this.toggleEditorBtn.addEventListener("click", () => this.toggleEditorFold());
    }

    this.consoleIframe = document.getElementById("console-iframe");

    // Code editor input & scroll sync for syntax highlighting
    if (this.codeEditor) {
      this.codeEditor.addEventListener("input", () => this.updateHighlighting());
      this.codeEditor.addEventListener("scroll", () => this.syncScroll());
      this.codeEditor.addEventListener("keydown", (e) => {
        if (e.key === "Tab") {
          e.preventDefault();
          const start = this.codeEditor.selectionStart;
          const end = this.codeEditor.selectionEnd;
          this.codeEditor.value = this.codeEditor.value.substring(0, start) + "  " + this.codeEditor.value.substring(end);
          this.codeEditor.selectionStart = this.codeEditor.selectionEnd = start + 2;
          this.updateHighlighting();
        }
      });
    }

    // Preset buttons
    document.querySelectorAll(".preset-btn").forEach(btn => {
      btn.addEventListener("click", () => {
        this.sqlInput.value = btn.getAttribute("data-sql");
        this.executeSql();
      });
    });

    this.runBtn.addEventListener("click", () => this.runWorkflow());
    this.failBtn.addEventListener("click", () => this.toggleFailure());
    this.resetBtn.addEventListener("click", () => this.resetDatabase());
    this.themeBtn.addEventListener("click", () => this.toggleTheme());
    this.sqlRunBtn.addEventListener("click", () => this.executeSql());
    this.demoSelect.addEventListener("change", (e) => this.switchDemo(e.target.value));

    // Enter key executes SQL in bar
    this.sqlInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") this.executeSql();
    });

    // Theme initialization and synchronization
    const savedTheme = localStorage.getItem("relay-theme") || "dark";
    this.setTheme(savedTheme, false);

    if (this.consoleIframe) {
      this.consoleIframe.addEventListener("load", () => {
        const theme = document.documentElement.getAttribute("data-theme") || "dark";
        this.consoleIframe.contentWindow?.postMessage({ type: "set_theme", theme }, "*");
      });
    }

    window.addEventListener("message", (event) => {
      if (event.data && event.data.type === "theme_changed") {
        this.setTheme(event.data.theme, false);
      }
    });

    // Initial code
    this.codeEditor.value = DEMO_SCRIPTS.checkout;
    this.updateHighlighting();
  }

  updateHighlighting() {
    if (!this.codeEditor || !this.codeHighlightingContent) return;
    const text = this.codeEditor.value;
    this.codeHighlightingContent.textContent = text + (text.endsWith("\n") ? "" : "\n");
    if (window.Prism) {
      window.Prism.highlightElement(this.codeHighlightingContent);
    }
  }

  syncScroll() {
    if (!this.codeEditor || !this.codeHighlighting) return;
    this.codeHighlighting.scrollTop = this.codeEditor.scrollTop;
    this.codeHighlighting.scrollLeft = this.codeEditor.scrollLeft;
  }

  log(msg, type = "info") {
    const time = new Date().toISOString().slice(11, 19);
    const line = document.createElement("div");
    line.className = `log-line ${type}`;
    line.textContent = `[${time}] ${msg}`;
    this.consoleLogs.appendChild(line);
    this.consoleLogs.scrollTop = this.consoleLogs.scrollHeight;
  }

  setTheme(theme, notifyIframe = true) {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("relay-theme", theme);
    if (this.themeBtn) {
      const label = this.themeBtn.querySelector(".theme-label");
      if (label) label.textContent = theme === "dark" ? "Light Mode" : "Dark Mode";
      const icon = this.themeBtn.querySelector("svg");
      if (icon) {
        icon.outerHTML = theme === "dark" ? `
          <svg class="theme-icon-sun" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>
        ` : `
          <svg class="theme-icon-moon" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
        `;
      }
    }
    if (notifyIframe && this.consoleIframe && this.consoleIframe.contentWindow) {
      this.consoleIframe.contentWindow.postMessage({ type: "set_theme", theme }, "*");
    }
  }

  toggleTheme() {
    const current = document.documentElement.getAttribute("data-theme") || "dark";
    const next = current === "dark" ? "light" : "dark";
    this.setTheme(next, true);
  }

  toggleFailure() {
    this.injectFailure = !this.injectFailure;
    if (this.injectFailure) {
      this.failBtn.classList.add("active");
      this.failBtn.innerHTML = `
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>
        <span>Step Failure: ON</span>
      `;
      this.log("Step failure injection armed: reserveInventory will throw InventoryShortageError", "warn");
    } else {
      this.failBtn.classList.remove("active");
      this.failBtn.innerHTML = `
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>
        <span>Inject Step Failure</span>
      `;
      this.log("Step failure injection cleared: normal workflow execution", "info");
    }
  }

  setDbStatus(text, state = "ready") {
    if (this.dbStatusText) this.dbStatusText.textContent = text;
    if (this.statusDot) {
      this.statusDot.className = `status-dot ${state}`;
    }
  }

  toggleEditorFold() {
    this.editorFolded = !this.editorFolded;
    localStorage.setItem("relay-playground-editor-folded", this.editorFolded);
    this.applyEditorFold(this.editorFolded);
  }

  applyEditorFold(folded) {
    if (this.leftPane) this.leftPane.classList.toggle("folded", folded);
    if (this.toggleEditorBtn) {
      this.toggleEditorBtn.innerHTML = folded ? `
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="15" y1="3" x2="15" y2="21"/></svg>
        <span>Show Code Panel</span>
      ` : `
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="9" y1="3" x2="9" y2="21"/></svg>
        <span>Fold Code Panel</span>
      `;
      this.toggleEditorBtn.title = folded ? "Expand code editor panel" : "Fold code editor panel";
    }
  }

  switchDemo(key) {
    if (DEMO_SCRIPTS[key]) {
      this.codeEditor.value = DEMO_SCRIPTS[key];
      this.updateHighlighting();
      this.log(`Switched demo script to: ${key}`, "info");
    }
  }

  async initDatabase() {
    this.log("Initializing in-browser PGlite (PostgreSQL 16 WebAssembly)...", "info");
    try {
      // Dynamic import PGlite from CDN
      const { PGlite } = await import("https://cdn.jsdelivr.net/npm/@electric-sql/pglite/dist/index.js");
      window.pgliteDb = new PGlite();
      this.db = window.pgliteDb;
      this.log("PGlite WASM engine started successfully in browser memory", "success");
      this.setDbStatus("PGlite (Postgres 16 WASM) Ready", "ready");

      await this.createSchema();
      await this.seedInitialData();
      await this.executeSql("SELECT * FROM dbos.workflow_status;");
    } catch (err) {
      console.error("PGlite initialization failed:", err);
      this.log(`PGlite CDN load failed: ${err.message}. Initializing fallback in-memory SQLite emulator...`, "warn");
      this.initFallbackEngine();
    }
  }

  initFallbackEngine() {
    // In-memory fallback if CDN is offline
    const store = {
      workflows: [],
      steps: [],
      events: []
    };

    window.pgliteDb = {
      query: async (sql, params = []) => {
        const s = sql.toLowerCase();
        if (s.includes("dbos.workflow_status")) {
          return { rows: store.workflows };
        }
        if (s.includes("dbos.operation_execution")) {
          if (params.length > 0) {
            return { rows: store.steps.filter(step => step.workflow_id === params[0]) };
          }
          return { rows: store.steps };
        }
        return { rows: [] };
      },
      exec: async (sql) => {}
    };
    this.db = window.pgliteDb;

    this.setDbStatus("In-Memory Engine Ready", "ready");
    this.seedInitialData();
  }

  async createSchema() {
    const ddl = `
      CREATE SCHEMA IF NOT EXISTS dbos;

      CREATE TABLE IF NOT EXISTS applications (
        name TEXT PRIMARY KEY,
        organization TEXT NOT NULL,
        status TEXT NOT NULL,
        runtime TEXT NOT NULL,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
      );

      CREATE TABLE IF NOT EXISTS executors (
        id TEXT PRIMARY KEY,
        name TEXT NOT NULL,
        status TEXT NOT NULL,
        hostname TEXT NOT NULL,
        ip_address TEXT NOT NULL,
        version TEXT NOT NULL,
        last_heartbeat TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
      );

      CREATE TABLE IF NOT EXISTS dbos.workflow_status (
        workflow_id TEXT PRIMARY KEY,
        status TEXT NOT NULL,
        name TEXT NOT NULL,
        class_name TEXT,
        config_name TEXT,
        authenticated_user TEXT DEFAULT 'playground-user',
        assumed_role TEXT,
        authenticated_roles JSONB DEFAULT '[]',
        request TEXT,
        output TEXT,
        error TEXT,
        executor_id TEXT,
        app_id TEXT DEFAULT 'ecommerce-checkout',
        app_version TEXT DEFAULT '1.0.0',
        application_version TEXT DEFAULT '1.0.0',
        application_id TEXT DEFAULT 'ecommerce-checkout',
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        duration_ms INTEGER DEFAULT 0
      );

      CREATE TABLE IF NOT EXISTS dbos.operation_execution (
        workflow_id TEXT NOT NULL,
        function_id INTEGER NOT NULL,
        name TEXT NOT NULL,
        type TEXT DEFAULT 'step',
        status TEXT NOT NULL,
        input TEXT,
        output TEXT,
        error TEXT,
        child_workflow_id TEXT,
        duration_ms INTEGER DEFAULT 0,
        created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (workflow_id, function_id)
      );

      CREATE TABLE IF NOT EXISTS dbos.workflow_events (
        workflow_id TEXT NOT NULL,
        key TEXT NOT NULL,
        value TEXT,
        PRIMARY KEY (workflow_id, key)
      );
    `;
    await window.pgliteDb.exec(ddl);
    this.log("DBOS system schema (dbos.workflow_status, dbos.operation_execution) created", "success");
  }

  async seedInitialData() {
    const now = new Date().toISOString();
    // Seed pre-populated workflow so DAG is visible immediately
    const wfId = "wf-ord-89214";
    const childAuthId = "wf-child-auth-01";
    const childShipId = "wf-child-ship-02";

    await window.pgliteDb.exec(`
      DELETE FROM dbos.workflow_status;
      DELETE FROM dbos.operation_execution;
      DELETE FROM applications;
      DELETE FROM executors;

      INSERT INTO applications (name, organization, status, runtime)
      VALUES ('ecommerce-checkout', 'default', 'AVAILABLE', 'typescript');

      INSERT INTO executors (id, name, status, hostname, ip_address, version)
      VALUES ('wasm-executor-1', 'ecommerce-checkout', 'healthy', 'browser-wasm', '127.0.0.1', '1.0.0');

      -- Root Workflow
      INSERT INTO dbos.workflow_status (workflow_id, status, name, duration_ms, authenticated_user, output)
      VALUES ('${wfId}', 'SUCCESS', 'ProcessCheckoutWorkflow', 1242, 'alice@example.com', '{"orderId":"${wfId}","status":"FULFILLED"}');

      -- Root Steps
      INSERT INTO dbos.operation_execution (workflow_id, function_id, name, status, output, duration_ms)
      VALUES
        ('${wfId}', 1, 'validateCart', 'SUCCESS', '{"valid":true,"items":2}', 310),
        ('${wfId}', 2, 'reserveInventory', 'SUCCESS', '{"reserved":true,"sku":"DBOS-RELAY-KEY"}', 245),
        ('${wfId}', 3, 'authorizePaymentGateway', 'SUCCESS', '{"authId":"${childAuthId}"}', 412),
        ('${wfId}', 4, 'scheduleShipping', 'SUCCESS', '{"carrier":"FastTrack","tracking":"FT-99124"}', 180),
        ('${wfId}', 5, 'sendReceiptNotification', 'SUCCESS', '{"delivered":true}', 95);

      -- Child 1: Payment
      INSERT INTO dbos.workflow_status (workflow_id, status, name, duration_ms, authenticated_user, output)
      VALUES ('${childAuthId}', 'SUCCESS', 'AuthorizePaymentGateway', 412, 'alice@example.com', '{"authorized":true}');

      INSERT INTO dbos.operation_execution (workflow_id, function_id, name, status, output, duration_ms)
      VALUES
        ('${childAuthId}', 1, 'checkFraudVelocity', 'SUCCESS', '{"score":10,"cleared":true}', 120),
        ('${childAuthId}', 2, 'chargeCard', 'SUCCESS', '{"card":"*4242","captured":true}', 292);

      -- Link Child 1 to Root Step 3
      UPDATE dbos.operation_execution SET child_workflow_id = '${childAuthId}' WHERE workflow_id = '${wfId}' AND function_id = 3;

      -- Child 2: Shipping
      INSERT INTO dbos.workflow_status (workflow_id, status, name, duration_ms, authenticated_user, output)
      VALUES ('${childShipId}', 'SUCCESS', 'ReserveWarehouseInventory', 180, 'alice@example.com', '{"allocated":true}');

      INSERT INTO dbos.operation_execution (workflow_id, function_id, name, status, output, duration_ms)
      VALUES
        ('${childShipId}', 1, 'locateOptimalBin', 'SUCCESS', '{"bin":"A-12-04"}', 85),
        ('${childShipId}', 2, 'lockInventorySlot', 'SUCCESS', '{"locked":true}', 95);

      -- Link Child 2 to Root Step 4
      UPDATE dbos.operation_execution SET child_workflow_id = '${childShipId}' WHERE workflow_id = '${wfId}' AND function_id = 4;
    `);

    this.log(`Pre-populated demo workflow ${wfId} with 2 child workflows into PGlite`, "info");
    this.refreshConsole();
  }

  refreshConsole() {
    channel.postMessage({
      type: "relay_telemetry",
      payload: { type: "workflow_update", timestamp: Date.now() }
    });
  }

  broadcastUpdate(event, data) {
    channel.postMessage({
      type: "relay_telemetry",
      payload: { type: event, data, timestamp: Date.now() }
    });
  }

  async runWorkflow() {
    if (this.isRunning) return;
    this.isRunning = true;
    this.runBtn.disabled = true;
    this.runBtn.innerHTML = `
      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="spin" aria-hidden="true"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
      <span>Executing...</span>
    `;

    const randomSuffix = Math.floor(10000 + Math.random() * 90000);
    const wfId = `wf-ord-${randomSuffix}`;
    const childAuthId = `wf-pay-${randomSuffix}`;
    this.activeWorkflowId = wfId;

    this.log(`--------------------------------------------------`, "step");
    this.log(`[DBOS] Starting workflow ProcessCheckoutWorkflow (${wfId})`, "step");

    try {
      // 1. Insert initial PENDING workflow
      await this.db.query(`
        INSERT INTO dbos.workflow_status (
          workflow_id, status, name, class_name, config_name,
          authenticated_user, assumed_role, authenticated_roles,
          request, output, error, executor_id, app_id, app_version,
          created_at, updated_at
        ) VALUES (
          $1, 'PENDING', 'ProcessCheckoutWorkflow', 'OrderService', 'default',
          'customer-portal', 'customer', '["customer"]',
          $2, NULL, NULL, 'exec-inbrowser-1', 'app-ecommerce', '1.0.0',
          $3, $3
        )
      `, [
        wfId,
        JSON.stringify({ orderId: wfId, items: ["dbos-pro-license", "relay-support-tier"], amount: 299 }),
        Date.now()
      ]);

      this.broadcastUpdate("workflow_created", { workflowId: wfId, status: "PENDING" });
      if (this.consoleIframe && this.consoleIframe.contentWindow) {
        this.consoleIframe.contentWindow.location.hash = `#/workflow/${wfId}`;
      }
      await this.executeSql();
      await this.sleep(450);

      // Step 1: Validate Cart
      this.log(`[Step 1/5] Executing validateCart...`, "step");
      await this.db.query(`
        INSERT INTO dbos.operation_execution (
          workflow_id, function_id, name, type, status,
          input, output, error, duration_ms, created_at
        ) VALUES (
          $1, 0, 'validateCart', 'step', 'SUCCESS',
          $2, $3, NULL, 450, $4
        )
      `, [
        wfId,
        JSON.stringify({ orderId: wfId, amount: 299 }),
        JSON.stringify({ valid: true, items: ["dbos-pro-license", "relay-support-tier"] }),
        Date.now()
      ]);
      this.log(`✓ Step 1: validateCart completed in 450ms`, "success");
      await this.executeSql();
      await this.sleep(400);

      // Step 2: Reserve Inventory (Failure Injection Target)
      this.log(`[Step 2/5] Executing reserveInventory...`, "step");
      if (this.injectFailure) {
        await this.db.query(`
          INSERT INTO dbos.operation_execution (
            workflow_id, function_id, name, type, status,
            input, output, error, duration_ms, created_at
          ) VALUES (
            $1, 1, 'reserveInventory', 'step', 'ERROR',
            $2, NULL, $3, 320, $4
          )
        `, [
          wfId,
          JSON.stringify({ sku: "DBOS-RELAY-KEY", qty: 2 }),
          JSON.stringify({ code: "INVENTORY_SHORTAGE", message: "Requested stock SKU DBOS-RELAY-KEY exhausted in regional warehouse" }),
          Date.now()
        ]);

        await this.db.query(`
          UPDATE dbos.workflow_status
          SET status = 'ERROR',
              error = $1,
              duration_ms = 1220,
              updated_at = $2
          WHERE workflow_id = $3
        `, [
          JSON.stringify({ message: "Step reserveInventory failed: InventoryShortageError" }),
          Date.now(),
          wfId
        ]);

        this.log(`✗ Step 2: reserveInventory FAILED (Simulated Failure Injected)`, "error");
        this.log(`[DBOS] Workflow ${wfId} entered ERROR state. Transaction rolled back cleanly.`, "error");
        this.broadcastUpdate("workflow_failed", { workflowId: wfId, status: "ERROR" });
        await this.executeSql();
        return;
      }

      await this.db.query(`
        INSERT INTO dbos.operation_execution (
          workflow_id, function_id, name, type, status,
          input, output, error, duration_ms, created_at
        ) VALUES (
          $1, 1, 'reserveInventory', 'step', 'SUCCESS',
          $2, $3, NULL, 400, $4
        )
      `, [
        wfId,
        JSON.stringify({ sku: "DBOS-RELAY-KEY", qty: 2 }),
        JSON.stringify({ reserved: true, sku: "DBOS-RELAY-KEY", warehouse: "us-west-primary" }),
        Date.now()
      ]);
      this.log(`✓ Step 2: reserveInventory completed in 400ms`, "success");
      await this.executeSql();
      await this.sleep(500);

      // Step 3: Child Workflow AuthorizePaymentGateway
      this.log(`[Step 3/5] Spawning Child Workflow AuthorizePaymentGateway (${childAuthId})...`, "step");
      await this.db.query(`
        INSERT INTO dbos.workflow_status (
          workflow_id, status, name, class_name, config_name,
          authenticated_user, assumed_role, authenticated_roles,
          request, output, error, executor_id, app_id, app_version,
          created_at, updated_at
        ) VALUES (
          $1, 'SUCCESS', 'AuthorizePaymentGateway', 'PaymentService', 'default',
          'customer-portal', 'customer', '["customer"]',
          $2, $3, NULL, 'exec-inbrowser-1', 'app-ecommerce', '1.0.0',
          $4, $4
        )
      `, [
        childAuthId,
        JSON.stringify({ parentId: wfId, amount: 299, currency: "USD" }),
        JSON.stringify({ authCode: "AUTH-89214-OK", gateway: "Stripe-Mock" }),
        Date.now()
      ]);

      await this.db.query(`
        INSERT INTO dbos.operation_execution (
          workflow_id, function_id, name, type, status,
          input, output, error, duration_ms, created_at, child_workflow_id
        ) VALUES (
          $1, 2, 'authorizePayment', 'child_workflow', 'SUCCESS',
          $2, $3, NULL, 650, $4, $5
        )
      `, [
        wfId,
        JSON.stringify({ amount: 299 }),
        JSON.stringify({ authorized: true, transactionId: "tx_mock_9921" }),
        Date.now(),
        childAuthId
      ]);
      this.log(`✓ Step 3: Child Workflow AuthorizePaymentGateway (${childAuthId}) completed in 650ms`, "success");
      await this.executeSql();
      await this.sleep(400);

      // Step 4: Schedule Carrier Shipping
      this.log(`[Step 4/5] Executing scheduleShipping...`, "step");
      await this.db.query(`
        INSERT INTO dbos.operation_execution (
          workflow_id, function_id, name, type, status,
          input, output, error, duration_ms, created_at
        ) VALUES (
          $1, 3, 'scheduleShipping', 'step', 'SUCCESS',
          $2, $3, NULL, 380, $4
        )
      `, [
        wfId,
        JSON.stringify({ carrier: "FastTrack", orderId: wfId }),
        JSON.stringify({ trackingNumber: "FT-9912048", status: "DISPATCHED" }),
        Date.now()
      ]);
      this.log(`✓ Step 4: scheduleShipping completed in 380ms (Tracking: FT-9912048)`, "success");
      await this.executeSql();
      await this.sleep(350);

      // Step 5: Send Receipt Notification
      this.log(`[Step 5/5] Executing sendReceiptNotification...`, "step");
      await this.db.query(`
        INSERT INTO dbos.operation_execution (
          workflow_id, function_id, name, type, status,
          input, output, error, duration_ms, created_at
        ) VALUES (
          $1, 4, 'sendReceiptNotification', 'step', 'SUCCESS',
          $2, $3, NULL, 220, $4
        )
      `, [
        wfId,
        JSON.stringify({ recipient: "operator@example.com", orderId: wfId }),
        JSON.stringify({ delivered: true, timestamp: Date.now() }),
        Date.now()
      ]);
      this.log(`✓ Step 5: sendReceiptNotification completed in 220ms`, "success");

      // Final: Complete parent workflow
      const totalDuration = 2100;
      await this.db.query(`
        UPDATE dbos.workflow_status
        SET status = 'SUCCESS',
            output = $1,
            duration_ms = $2,
            updated_at = $3
        WHERE workflow_id = $4
      `, [
        JSON.stringify({ status: "ORDER_FULFILLED", orderId: wfId, invoiceUrl: `https://relay.local/invoices/${wfId}` }),
        totalDuration,
        Date.now(),
        wfId
      ]);

      this.log(`✓ [DBOS] Workflow ${wfId} completed with status SUCCESS (${totalDuration}ms total duration)`, "success");
      this.broadcastUpdate("workflow_completed", { workflowId: wfId, status: "SUCCESS" });
      await this.executeSql();

    } catch (err) {
      this.log(`Execution error: ${err.message}`, "error");
    } finally {
      this.isRunning = false;
      this.runBtn.disabled = false;
      this.runBtn.innerHTML = `
        <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><polygon points="5 3 19 12 5 21 5 3"/></svg>
        <span>Run Workflow</span>
      `;
    }
  }

  async executeSql(overrideQuery = null) {
    const query = (overrideQuery || this.sqlInput.value).trim();
    if (!query) return;

    try {
      const res = await window.pgliteDb.query(query);
      this.renderTable(res.rows);
    } catch (err) {
      this.queryResults.innerHTML = `<div style="color: var(--accent-red); padding: 12px; font-family: var(--font-mono); font-size: 12px;">SQL Error: ${err.message}</div>`;
    }
  }

  renderTable(rows) {
    if (!rows || rows.length === 0) {
      this.queryResults.innerHTML = `<div style="color: var(--text-muted); padding: 12px; font-size: 12px;">(0 rows returned)</div>`;
      return;
    }

    const cols = Object.keys(rows[0]);
    let html = `<table class="data-table"><thead><tr>`;
    cols.forEach(c => html += `<th>${c}</th>`);
    html += `</tr></thead><tbody>`;

    rows.forEach(r => {
      html += `<tr>`;
      cols.forEach(c => {
        let val = r[c];
        if (typeof val === "object" && val !== null) val = JSON.stringify(val);
        html += `<td>${val === null || val === undefined ? '<span style="color: var(--text-muted)">null</span>' : val}</td>`;
      });
      html += `</tr>`;
    });

    html += `</tbody></table>`;
    this.queryResults.innerHTML = html;
  }

  async resetDatabase() {
    this.log("Resetting in-browser database to pristine demo state...", "info");
    await this.seedInitialData();
    await this.executeSql("SELECT * FROM dbos.workflow_status;");
    if (this.consoleIframe && this.consoleIframe.contentWindow) {
      this.consoleIframe.contentWindow.location.hash = "#fleet";
    }
    this.log("Database reset complete", "success");
  }

  sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
  }
}

// Bootstrap
window.addEventListener("DOMContentLoaded", () => {
  window.playground = new PlaygroundController();
});
