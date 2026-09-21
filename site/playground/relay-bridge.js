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
      { name: "orders-vip", appName: "ecommerce-checkout", concurrency: 20, workerConcurrency: 4, partitionQueue: false, activeCount: 1 },
      { name: "orders-standard", appName: "ecommerce-checkout", concurrency: 50, workerConcurrency: 5, partitionQueue: false, activeCount: 2 }
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
    appConfigs: {
      "ecommerce-checkout": {
        privateMode: false,
        executorTimeoutSecs: 10,
        gcRowsThreshold: null,
        gcTimeThresholdMs: null,
        globalTimeoutMs: null
      }
    },
    autoscaling: {},
    auditLog: [
      {
        id: "audit-seed-1",
        emitTime: "2026-09-18T18:05:00Z",
        operation: "workflow.cancel",
        status: "success",
        subject: { type: "user", id: "user_alice", display: "alice@example.com" },
        target: { type: "workflow", id: "wf-ord-89214" },
        sourceIp: "127.0.0.1",
        details: { application_name: "ecommerce-checkout" }
      },
      {
        id: "audit-seed-2",
        emitTime: "2026-09-18T18:06:00Z",
        operation: "token.create",
        status: "success",
        subject: { type: "api_key", id: "key-1", display: "dev-local-key" },
        target: { type: "token", id: "dev-local-key" },
        sourceIp: "127.0.0.1",
        details: {}
      }
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
        updatedAt: "2026-09-18T18:00:01.242Z",
        updated_at: "2026-09-18T18:00:01.242Z",
        completedAt: "2026-09-18T18:00:01.242Z",
        completed_at: "2026-09-18T18:00:01.242Z",
        durationMs: 1242,
        duration_ms: 1242,
        authenticatedUser: "alice@example.com",
        authenticated_user: "alice@example.com",
        output: '{"orderId":"wf-ord-89214","status":"FULFILLED"}'
      }
    ],
    steps: [
      { functionId: 1, function_id: 1, stepId: "1", stepName: "validateCart", name: "validateCart", status: "SUCCESS", output: '{"valid":true,"items":2}', durationMs: 310, duration_ms: 310, startedAt: "2026-09-18T18:00:00.000Z", completedAt: "2026-09-18T18:00:00.310Z" },
      { functionId: 2, function_id: 2, stepId: "2", stepName: "reserveInventory", name: "reserveInventory", status: "SUCCESS", output: '{"reserved":true,"sku":"DBOS-RELAY-KEY"}', durationMs: 245, duration_ms: 245, startedAt: "2026-09-18T18:00:00.310Z", completedAt: "2026-09-18T18:00:00.555Z" },
      { functionId: 3, function_id: 3, stepId: "3", stepName: "authorizePaymentGateway", name: "authorizePaymentGateway", status: "SUCCESS", output: '{"authId":"wf-child-auth-01"}', childWorkflowId: "wf-child-auth-01", child_workflow_id: "wf-child-auth-01", durationMs: 412, duration_ms: 412, startedAt: "2026-09-18T18:00:00.555Z", completedAt: "2026-09-18T18:00:00.967Z" },
      { functionId: 4, function_id: 4, stepId: "4", stepName: "scheduleShipping", name: "scheduleShipping", status: "SUCCESS", output: '{"carrier":"FastTrack","tracking":"FT-99124"}', durationMs: 180, duration_ms: 180, startedAt: "2026-09-18T18:00:00.967Z", completedAt: "2026-09-18T18:00:01.147Z" },
      { functionId: 5, function_id: 5, stepId: "5", stepName: "sendReceiptNotification", name: "sendReceiptNotification", status: "SUCCESS", output: '{"delivered":true}', durationMs: 95, duration_ms: 95, startedAt: "2026-09-18T18:00:01.147Z", completedAt: "2026-09-18T18:00:01.242Z" }
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

  // Audit entries for playground-made mutations (no-auth demo: single actor)
  let auditSeq = 100;
  function pushAudit(operation, status, target) {
    state.auditLog.unshift({
      id: "audit-play-" + (auditSeq++),
      emitTime: new Date().toISOString(),
      operation: operation,
      status: status,
      subject: { type: "user", id: "user_playground", display: "playground-user" },
      target: target || null,
      sourceIp: "127.0.0.1",
      details: {}
    });
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
      const data = (rows || []).map(r => {
        const isTerminal = ["SUCCESS", "ERROR", "CANCELLED"].includes(String(r.status || "").toUpperCase());
        const dur = r.duration_ms || 0;
        let completedAt = null;
        if (isTerminal) {
          if (r.updated_at) {
            completedAt = r.updated_at;
          } else if (r.created_at) {
            completedAt = new Date(new Date(r.created_at).getTime() + dur).toISOString();
          }
        }
        return {
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
          completedAt: completedAt,
          completed_at: completedAt,
          durationMs: dur,
          duration_ms: dur,
          authenticatedUser: r.authenticated_user || "playground-user",
          authenticated_user: r.authenticated_user || "playground-user",
          output: r.output,
          error: r.error
        };
      });
      return new Response(JSON.stringify(data), { status: 200, headers: jsonHeaders });
    }

    // 5. /v2/orgs/{org}/apps/{app}/workflows/{id}/steps (Workflow DAG steps)
    const stepsMatch = path.match(/^\/v2\/orgs\/[^/]+\/apps\/[^/]+\/workflows\/([^/]+)\/steps$/);
    if (stepsMatch) {
      const workflowId = stepsMatch[1];
      const rows = await queryDb(
        "SELECT function_id, name, status, output, error, child_workflow_id, duration_ms, created_at " +
        "FROM dbos.operation_execution WHERE workflow_id = $1 ORDER BY function_id ASC;",
        [workflowId]
      );
      const data = (rows && rows.length > 0) ? rows.map(r => {
        const dur = r.duration_ms || 100;
        const createdAt = r.created_at ? new Date(r.created_at).toISOString() : new Date().toISOString();
        const isSuccess = (r.status || "SUCCESS").toUpperCase() === "SUCCESS";
        return {
          functionId: r.function_id,
          function_id: r.function_id,
          stepId: r.function_id,
          stepName: r.name,
          name: r.name,
          status: (r.status || "SUCCESS").toUpperCase(),
          output: r.output,
          error: r.error,
          childWorkflowId: r.child_workflow_id,
          child_workflow_id: r.child_workflow_id,
          durationMs: dur,
          duration_ms: dur,
          startedAt: createdAt,
          completedAt: isSuccess ? new Date(new Date(createdAt).getTime() + dur).toISOString() : null
        };
      }) : (workflowId === "wf-ord-89214" ? state.steps : []);
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
        const isTerminal = ["SUCCESS", "ERROR", "CANCELLED"].includes(String(r.status || "").toUpperCase());
        const dur = r.duration_ms || 0;
        let completedAt = null;
        if (isTerminal) {
          if (r.updated_at) {
            completedAt = r.updated_at;
          } else if (r.created_at) {
            completedAt = new Date(new Date(r.created_at).getTime() + dur).toISOString();
          }
        }
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
          completedAt: completedAt,
          completed_at: completedAt,
          durationMs: dur,
          duration_ms: dur,
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
        pushAudit("token.revoke", "success", { type: "token", id: name });
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
        pushAudit("token.create", "success", { type: "token", id: name });
        return new Response(JSON.stringify(newKey), { status: 201, headers: jsonHeaders });
      }
    }

    // 13. Cancel, Resume, Fork endpoints
    if (path.includes("/cancel") || path.includes("/resume") || path.includes("/fork")) {
      const wfMatch = path.match(/\/workflows\/([^/]+)\/(cancel|resume|fork)$/);
      if (!wfMatch) {
        return new Response(JSON.stringify({ title: "Not Found", status: 404, detail: "unknown workflow operation" }), { status: 404, headers: jsonHeaders });
      }
      const op = "workflow." + (wfMatch[2] === "fork" ? "fork" : wfMatch[2]);
      const target = { type: "workflow", id: decodeURIComponent(wfMatch[1]) };
      pushAudit(op, "success", target);
      return new Response(JSON.stringify({ status: "SUCCESS" }), { status: 200, headers: jsonHeaders });
    }

    // 14. Single application details (merges stored settings over the registry row)
    const appMatch = path.match(/^\/v2\/orgs\/[^/]+\/apps\/([^/]+)$/);
    if (appMatch && (method === "GET" || method === "PATCH")) {
      const appName = decodeURIComponent(appMatch[1]);
      let app = state.apps.find(a => a.name === appName);
      if (!app) {
        const rows = await queryDb("SELECT name, organization, status, runtime, created_at FROM applications WHERE name = $1;", [appName]);
        if (rows && rows.length > 0) {
          const r = rows[0];
          app = { name: r.name, organization: r.organization, status: r.status, runtime: r.runtime, createdAt: r.created_at };
        }
      }
      if (!app) {
        return new Response(JSON.stringify({ title: "Not Found", status: 404, detail: "application not found" }), { status: 404, headers: jsonHeaders });
      }
      if (!state.appConfigs[appName]) {
        state.appConfigs[appName] = { privateMode: false, executorTimeoutSecs: 10, gcRowsThreshold: null, gcTimeThresholdMs: null, globalTimeoutMs: null };
      }
      if (method === "PATCH") {
        let body = {};
        try { if (init && init.body) body = JSON.parse(init.body); } catch (_) {}
        const cfg = state.appConfigs[appName];
        let changed = false;
        const apply = (key, pred) => {
          if (pred(body[key])) { cfg[key] = body[key]; changed = true; }
        };
        apply("privateMode", v => typeof v === "boolean");
        apply("executorTimeoutSecs", v => typeof v === "number");
        apply("gcRowsThreshold", v => typeof v === "number" || v === null);
        apply("gcTimeThresholdMs", v => typeof v === "number" || v === null);
        apply("globalTimeoutMs", v => typeof v === "number" || v === null);
        if (changed) {
          pushAudit("application.update", "success", { type: "application", id: appName });
        }
        return new Response(null, { status: 204 });
      }
      return new Response(JSON.stringify(Object.assign({}, app, state.appConfigs[appName])), { status: 200, headers: jsonHeaders });
    }

    // 15. Autoscaling policy (per-application, validated against known queues)
    const policyMatch = path.match(/^\/v2\/orgs\/[^/]+\/apps\/([^/]+)\/autoscaling-policy$/);
    if (policyMatch) {
      const appName = decodeURIComponent(policyMatch[1]);
      if (method === "GET") {
        const stored = state.autoscaling[appName];
        if (!stored) {
          return new Response(JSON.stringify({ title: "Not Found", status: 404, detail: "no autoscaling policy configured for application" }), { status: 404, headers: jsonHeaders });
        }
        return new Response(JSON.stringify({ policy: stored }), { status: 200, headers: jsonHeaders });
      }
      if (method === "PUT") {
        let body = {};
        try { if (init && init.body) body = JSON.parse(init.body); } catch (_) {}
        if (!body.queue) {
          return new Response(JSON.stringify({ title: "Bad Request", status: 400, detail: "autoscaling policy must name a queue" }), { status: 400, headers: jsonHeaders });
        }
        const queue = state.queues.find(q => q.name === body.queue);
        if (!queue) {
          return new Response(JSON.stringify({ title: "Bad Request", status: 400, detail: "unknown queue" }), { status: 400, headers: jsonHeaders });
        }
        if (queue.partitionQueue) {
          return new Response(JSON.stringify({ title: "Bad Request", status: 400, detail: "queue is partitioned and cannot drive autoscaling" }), { status: 400, headers: jsonHeaders });
        }
        if (!(queue.workerConcurrency > 0)) {
          return new Response(JSON.stringify({ title: "Bad Request", status: 400, detail: "queue has no worker concurrency set" }), { status: 400, headers: jsonHeaders });
        }
        const stored = { queue: body.queue };
        if (body.rollout) stored.rollout = body.rollout;
        if (body.$schema !== undefined) stored.$schema = body.$schema;
        state.autoscaling[appName] = stored;
        pushAudit("autoscaling_policy.set", "success", { type: "application", id: appName });
        return new Response(JSON.stringify({ policy: stored }), { status: 200, headers: jsonHeaders });
      }
      if (method === "DELETE") {
        const removed = Boolean(state.autoscaling[appName]);
        delete state.autoscaling[appName];
        if (removed) {
          pushAudit("autoscaling_policy.delete", "success", { type: "application", id: appName });
        }
        return new Response(null, { status: 204 });
      }
    }

    // 16. Autoscale recommendations (simplified playground computation)
    const autoscaleMatch = path.match(/^\/v2\/orgs\/[^/]+\/apps\/([^/]+)\/autoscale(\/versions\/(.+))?$/);
    if (autoscaleMatch && method === "GET") {
      const appName = decodeURIComponent(autoscaleMatch[1]);
      const policy = state.autoscaling[appName];
      if (!policy) {
        return new Response(JSON.stringify({ title: "Not Found", status: 404, detail: "no autoscaling policy configured for application" }), { status: 404, headers: jsonHeaders });
      }
      const queue = state.queues.find(q => q.name === policy.queue) || {};
      const workerConcurrency = queue.workerConcurrency > 0 ? queue.workerConcurrency : 1;
      let depth = 0;
      try {
        const rows = await queryDb("SELECT COUNT(*) AS n FROM dbos.workflow_status WHERE status IN ('ENQUEUED','PENDING');");
        if (rows && rows.length > 0 && rows[0].n !== undefined) depth = Number(rows[0].n) || 0;
      } catch (_) {}
      let desired = Math.max(1, Math.ceil(depth / workerConcurrency));
      if (queue.concurrency > 0) {
        desired = Math.min(desired, Math.ceil(queue.concurrency / workerConcurrency));
      }
      const rec = {
        applicationVersion: "1.0.0",
        isLatest: true,
        desiredExecutors: desired,
        queueName: policy.queue,
        queueDepth: depth,
        observedAt: Date.now()
      };
      if (autoscaleMatch[3]) {
        const version = decodeURIComponent(autoscaleMatch[3]);
        if (version !== "latest" && version !== "1.0.0") {
          return new Response(JSON.stringify({ title: "Not Found", status: 404, detail: "application version was never registered" }), { status: 404, headers: jsonHeaders });
        }
        return new Response(JSON.stringify(rec), { status: 200, headers: jsonHeaders });
      }
      return new Response(JSON.stringify([rec]), { status: 200, headers: jsonHeaders });
    }

    // 17. Audit log (newest first, filters, paging)
    if (path.match(/^\/v2\/orgs\/[^/]+\/audit-logs$/)) {
      const q = parsed.searchParams;
      const op = q.get("operation") || "";
      const subject = q.get("subject") || "";
      const target = q.get("target") || "";
      const startTime = q.get("startTime") || "";
      const endTime = q.get("endTime") || "";
      const limitRaw = parseInt(q.get("limit") || "100", 10);
      const offsetRaw = parseInt(q.get("offset") || "0", 10);
      if (!Number.isInteger(limitRaw) || !Number.isInteger(offsetRaw) || limitRaw < 0 || offsetRaw < 0 || limitRaw > 1000) {
        return new Response(JSON.stringify({ title: "Bad Request", status: 400, detail: "invalid limit or offset" }), { status: 400, headers: jsonHeaders });
      }
      const limit = limitRaw === 0 ? 100 : limitRaw;
      const offset = offsetRaw;
      let rows = state.auditLog.slice();
      if (op) rows = rows.filter(e => e.operation === op);
      if (subject) rows = rows.filter(e => (e.subject && (e.subject.display === subject || e.subject.id === subject)) || e.username === subject);
      if (target) rows = rows.filter(e => e.target && e.target.id === target);
      if (startTime) rows = rows.filter(e => e.emitTime >= startTime);
      if (endTime) rows = rows.filter(e => e.emitTime < endTime);
      rows.sort((a, b) => (a.emitTime < b.emitTime ? 1 : -1));
      return new Response(JSON.stringify(rows.slice(offset, offset + limit)), { status: 200, headers: jsonHeaders });
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
