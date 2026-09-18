// relay-bridge.js - In-browser API & SSE bridge for embedded Relay Console
// Intercepts fetch() and EventSource() calls inside the console iframe
// and routes them to the in-memory PGlite / storage database.

(function () {
  const nativeFetch = window.fetch;
  const channel = new BroadcastChannel("relay-playground-bus");

  // In-memory fallback state if PGlite is still initializing
  const state = {
    apps: [
      {
        name: "ecommerce-checkout",
        organization: "default",
        status: "AVAILABLE",
        runtime: "typescript",
        createdAt: "2026-09-18T18:00:00Z"
      }
    ],
    executors: [
      {
        executorId: "wasm-executor-1",
        id: "wasm-executor-1",
        name: "ecommerce-checkout",
        appName: "ecommerce-checkout",
        status: "HEALTHY",
        hostname: "browser-wasm",
        ipAddress: "127.0.0.1",
        version: "1.0.0",
        appVersion: "1.0.0",
        language: "typescript",
        createdAt: "2026-09-18T18:00:00Z",
        updatedAt: new Date().toISOString(),
        lastHeartbeat: new Date().toISOString()
      }
    ],
    queues: [
      { name: "orders-vip", appName: "ecommerce-checkout", concurrency: 20, activeCount: 1 },
      { name: "orders-standard", appName: "ecommerce-checkout", concurrency: 50, activeCount: 2 }
    ],
    schedules: [
      { name: "daily-reconciliation", appName: "ecommerce-checkout", schedule: "0 0 * * *", workflowName: "ReconciliationWorkflow", status: "ACTIVE", lastRun: new Date().toISOString() }
    ],
    alerts: [
      { id: "rule-1", name: "High Error Rate", appName: "ecommerce-checkout", metric: "workflow_failure_rate", condition: "gt", threshold: 5, status: "ACTIVE" }
    ],
    keys: [
      { id: "key-1", tokenName: "dev-local-key", name: "dev-local-key", prefix: "dbos_sec_dev", createdAt: "2026-09-18T18:00:00Z", permissions: ["*"], appIds: ["*"], appNames: ["*"] }
    ],
    workflows: [
      {
        workflowId: "wf-ord-89214",
        workflow_id: "wf-ord-89214",
        workflowName: "ProcessCheckoutWorkflow",
        workflow_name: "ProcessCheckoutWorkflow",
        name: "ProcessCheckoutWorkflow",
        status: "SUCCESS",
        createdAt: "2026-09-18T18:00:00Z",
        created_at: "2026-09-18T18:00:00Z",
        updatedAt: "2026-09-18T18:00:01Z",
        updated_at: "2026-09-18T18:00:01Z",
        durationMs: 1242,
        duration_ms: 1242,
        authenticatedUser: "alice@example.com",
        authenticated_user: "alice@example.com",
        output: '{"orderId":"wf-ord-89214","status":"FULFILLED"}'
      }
    ],
    steps: [
      { functionId: 1, function_id: 1, stepId: "1", name: "validateCart", status: "SUCCESS", output: '{"valid":true,"items":2}', durationMs: 310, duration_ms: 310 },
      { functionId: 2, function_id: 2, stepId: "2", name: "reserveInventory", status: "SUCCESS", output: '{"reserved":true,"sku":"DBOS-RELAY-KEY"}', durationMs: 245, duration_ms: 245 },
      { functionId: 3, function_id: 3, stepId: "3", name: "authorizePaymentGateway", status: "SUCCESS", output: '{"authId":"wf-child-auth-01"}', childWorkflowId: "wf-child-auth-01", child_workflow_id: "wf-child-auth-01", durationMs: 412, duration_ms: 412 },
      { functionId: 4, function_id: 4, stepId: "4", name: "scheduleShipping", status: "SUCCESS", output: '{"carrier":"FastTrack","tracking":"FT-99124"}', durationMs: 180, duration_ms: 180 },
      { functionId: 5, function_id: 5, stepId: "5", name: "sendReceiptNotification", status: "SUCCESS", output: '{"delivered":true}', durationMs: 95, duration_ms: 95 }
    ]
  };

  // Helper to query parent PGlite instance if available
  async function queryDb(sql, params = []) {
    const db = (window.parent && window.parent.pgliteDb) || window.pgliteDb;
    if (db) {
      try {
        const res = await db.query(sql, params);
        return res.rows;
      } catch (err) {
        console.warn("[RelayBridge] SQL query error, falling back:", err);
      }
    }
    return null;
  }

  // Custom EventSource polyfill for SSE telemetry
  class MockEventSource {
    constructor(url) {
      this.url = url;
      this.readyState = 1; // OPEN
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;

      this.handler = (event) => {
        if (this.onmessage && event.data && event.data.type === "relay_telemetry") {
          this.onmessage({
            data: JSON.stringify(event.data.payload)
          });
        }
      };

      channel.addEventListener("message", this.handler);
      setTimeout(() => {
        if (this.onopen) this.onopen({ type: "open" });
      }, 50);
    }

    close() {
      this.readyState = 2;
      channel.removeEventListener("message", this.handler);
    }
  }

  window.EventSource = MockEventSource;

  // Intercept fetch
  window.fetch = async function (input, init) {
    const url = typeof input === "string" ? input : input.url;
    const parsed = new URL(url, window.location.href);
    const path = parsed.pathname;
    const method = (init && init.method ? init.method : (typeof input === "object" && input.method ? input.method : "GET")).toUpperCase();

    // Static assets bypass
    if (path.includes("/assets/") || path.endsWith(".js") || path.endsWith(".css") || path.endsWith(".svg") || path.endsWith(".png")) {
      return nativeFetch(input, init);
    }

    // Standard headers
    const jsonHeaders = { "Content-Type": "application/json" };

    // 1. /v2/users/me -> 404 Problem Details (Self-hosted no-auth mode signal)
    if (path === "/v2/users/me" || path === "/v2/users") {
      return new Response(
        JSON.stringify({
          type: "https://relay.dbos.dev/errors/not-found",
          title: "Not Found",
          status: 404,
          detail: "User endpoint disabled in self-hosted no-auth playground mode"
        }),
        { status: 404, headers: jsonHeaders }
      );
    }

    // 2. /v2/orgs/{org}/apps
    if (path.match(/^\/v2\/orgs\/[^/]+\/apps$/)) {
      const rows = await queryDb("SELECT name, organization, status, runtime, created_at FROM applications;");
      const data = rows && rows.length > 0 ? rows.map(r => ({
        name: r.name,
        organization: r.organization,
        status: r.status,
        runtime: r.runtime,
        createdAt: r.created_at
      })) : state.apps;
      return new Response(JSON.stringify(data), { status: 200, headers: jsonHeaders });
    }

    // 3. /v2/orgs/{org}/apps/{app}/executors
    if (path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/executors$/)) {
      const rows = await queryDb("SELECT id, name, status, hostname, ip_address, version, last_heartbeat FROM executors;");
      const data = rows && rows.length > 0 ? rows.map(r => ({
        executorId: r.id,
        id: r.id,
        name: r.name || "ecommerce-checkout",
        appName: r.name || "ecommerce-checkout",
        status: (r.status || "HEALTHY").toUpperCase(),
        hostname: r.hostname || "browser-wasm",
        ipAddress: r.ip_address,
        version: r.version || "1.0.0",
        appVersion: r.version || "1.0.0",
        language: "typescript",
        createdAt: "2026-09-18T18:00:00Z",
        updatedAt: r.last_heartbeat || new Date().toISOString(),
        lastHeartbeat: r.last_heartbeat || new Date().toISOString()
      })) : state.executors;
      return new Response(JSON.stringify(data), { status: 200, headers: jsonHeaders });
    }

    // 4. /v2/orgs/{org}/apps/{app}/workflows (List workflows)
    if (path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/workflows$/)) {
      const rows = await queryDb(
        "SELECT workflow_id, status, name, authenticated_user, output, error, duration_ms, created_at, updated_at " +
        "FROM dbos.workflow_status ORDER BY created_at DESC LIMIT 50;"
      );
      const data = (rows || []).map(r => ({
        workflowId: r.workflow_id,
        workflow_id: r.workflow_id,
        workflowName: r.name,
        workflow_name: r.name,
        name: r.name,
        status: r.status,
        createdAt: r.created_at,
        created_at: r.created_at,
        updatedAt: r.updated_at,
        updated_at: r.updated_at,
        durationMs: r.duration_ms || 0,
        duration_ms: r.duration_ms || 0,
        authenticatedUser: r.authenticated_user || "playground-user",
        authenticated_user: r.authenticated_user || "playground-user",
        output: r.output,
        error: r.error
      }));
      return new Response(JSON.stringify(data), { status: 200, headers: jsonHeaders });
    }

    // 5. /v2/orgs/{org}/apps/{app}/workflows/{id}/steps (Workflow DAG steps)
    const stepsMatch = path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/workflows\/([^/]+)\/steps$/);
    if (stepsMatch) {
      const workflowId = stepsMatch[1];
      const rows = await queryDb(
        "SELECT function_id, name, status, output, error, child_workflow_id, duration_ms " +
        "FROM dbos.operation_execution WHERE workflow_id = $1 ORDER BY function_id ASC;",
        [workflowId]
      );
      const data = (rows || []).map(r => ({
        functionId: r.function_id,
        function_id: r.function_id,
        stepId: String(r.function_id),
        name: r.name,
        status: r.status,
        output: r.output,
        error: r.error,
        childWorkflowId: r.child_workflow_id,
        child_workflow_id: r.child_workflow_id,
        durationMs: r.duration_ms || 0,
        duration_ms: r.duration_ms || 0
      }));
      return new Response(JSON.stringify(data), { status: 200, headers: jsonHeaders });
    }

    // 6. /v2/orgs/{org}/apps/{app}/workflows/{id}/events
    const eventsMatch = path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/workflows\/([^/]+)\/events$/);
    if (eventsMatch) {
      const workflowId = eventsMatch[1];
      const rows = await queryDb(
        "SELECT key, value FROM dbos.workflow_events WHERE workflow_id = $1;",
        [workflowId]
      );
      return new Response(JSON.stringify(rows || []), { status: 200, headers: jsonHeaders });
    }

    // 7. /v2/orgs/{org}/apps/{app}/workflows/{id}/notifications
    const notifsMatch = path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/workflows\/([^/]+)\/notifications$/);
    if (notifsMatch) {
      return new Response(JSON.stringify([]), { status: 200, headers: jsonHeaders });
    }

    // 8. /v2/orgs/{org}/apps/{app}/workflows/{id} (Single workflow details)
    const singleWfMatch = path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/workflows\/([^/]+)$/);
    if (singleWfMatch) {
      const workflowId = singleWfMatch[1];
      const rows = await queryDb(
        "SELECT workflow_id, status, name, authenticated_user, output, error, duration_ms, created_at, updated_at " +
        "FROM dbos.workflow_status WHERE workflow_id = $1;",
        [workflowId]
      );
      if (rows && rows.length > 0) {
        const r = rows[0];
        const data = {
          workflowId: r.workflow_id,
          workflow_id: r.workflow_id,
          workflowName: r.name,
          workflow_name: r.name,
          name: r.name,
          status: r.status,
          createdAt: r.created_at,
          created_at: r.created_at,
          updatedAt: r.updated_at,
          updated_at: r.updated_at,
          durationMs: r.duration_ms || 0,
          duration_ms: r.duration_ms || 0,
          authenticatedUser: r.authenticated_user || "playground-user",
          authenticated_user: r.authenticated_user || "playground-user",
          output: r.output,
          error: r.error
        };
        return new Response(JSON.stringify(data), { status: 200, headers: jsonHeaders });
      }
      const fallbackWf = (state.workflows || []).find(w => (w.workflowId || w.workflow_id) === workflowId);
      if (fallbackWf) {
        return new Response(JSON.stringify(fallbackWf), { status: 200, headers: jsonHeaders });
      }
      return new Response(JSON.stringify({ title: "Workflow not found", status: 404 }), { status: 404, headers: jsonHeaders });
    }

    // 9. /v2/orgs/{org}/apps/{app}/queues
    if (path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/queues$/)) {
      return new Response(JSON.stringify(state.queues), { status: 200, headers: jsonHeaders });
    }

    // 10. /v2/orgs/{org}/apps/{app}/schedules
    if (path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/schedules$/)) {
      return new Response(JSON.stringify(state.schedules), { status: 200, headers: jsonHeaders });
    }

    // 11. /v2/orgs/{org}/apps/{app}/alerts
    if (path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/alerts$/)) {
      return new Response(JSON.stringify(state.alerts), { status: 200, headers: jsonHeaders });
    }

    // 12. /v2/orgs/{org}/tokens or /keys
    if (path.match(/^\/v2\/orgs\/[^/]+\/(tokens|keys)$/)) {
      if (method === "GET") {
        return new Response(JSON.stringify(state.keys), { status: 200, headers: jsonHeaders });
      }
    }
    const tokenMatch = path.match(/^\/v2\/orgs\/[^/]+\/tokens\/([^/]+)$/);
    if (tokenMatch) {
      const name = decodeURIComponent(tokenMatch[1]);
      if (method === "DELETE") {
        state.keys = state.keys.filter(k => (k.tokenName !== name && k.name !== name));
        return new Response(null, { status: 204 });
      }
      if (method === "POST") {
        let body = {};
        try { if (init && init.body) body = JSON.parse(init.body); } catch (_) {}
        const newKey = {
          id: "key-" + Date.now(),
          tokenName: name,
          name: name,
          prefix: "dbos_sec_browser",
          token: "dbos_sec_" + Math.random().toString(36).substring(2, 15),
          createdAt: new Date().toISOString(),
          permissions: body.permissions || ["*"],
          appIds: body.appNames || ["*"],
          appNames: body.appNames || ["*"]
        };
        state.keys.push(newKey);
        return new Response(JSON.stringify(newKey), { status: 201, headers: jsonHeaders });
      }
    }

    // 13. Cancel, Resume, Fork endpoints
    if (path.includes("/cancel") || path.includes("/resume") || path.includes("/fork")) {
      return new Response(JSON.stringify({ status: "SUCCESS" }), { status: 200, headers: jsonHeaders });
    }

    return nativeFetch(input, init);
  };

  channel.onmessage = (event) => {
    if (event.data && event.data.type === "relay_telemetry") {
      if (window.app && typeof window.app.renderContentView === "function") {
        window.app.renderContentView();
      }
    }
  };

  console.log("[RelayBridge] In-browser mock API and SSE bridge initialized");
})();
