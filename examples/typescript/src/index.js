const crypto = require("crypto");

const appName = process.env.DBOS_APP_NAME || "typescript-sample-app";
const relayURL = process.env.RELAY_URL || "ws://localhost:8090";
const apiKey = process.env.RELAY_API_KEY || "dbos_sec_live_key";
const execID = "exec-ts-" + crypto.randomUUID().slice(0, 8);

const wsScheme = relayURL.startsWith("wss") ? "wss" : (relayURL.startsWith("ws") ? "ws" : (relayURL.startsWith("https") ? "wss" : "ws"));
const hostPart = relayURL.replace(/^(wss?|https?):\/\//, "");
const wsUrl = `${wsScheme}://${hostPart}/websocket/${appName}/${apiKey}`;

// Standard DBOS SDK startup log
console.log(`time=${new Date().toISOString()} level=INFO msg="DBOS launched" app_version=v1.0.0 executor_id=${execID} language=typescript`);

const ws = new WebSocket(wsUrl);

ws.addEventListener("open", () => {
  // Connected
});

ws.addEventListener("message", (event) => {
  try {
    const data = typeof event.data === "string" ? event.data : event.data.toString();
    const msg = JSON.parse(data);
    if (msg.type === "executor_info") {
      ws.send(JSON.stringify({
        type: "executor_info",
        request_id: msg.request_id,
        executor_id: execID,
        app_version: "v1.0.0",
        language: "typescript",
        dbos_version: "0.1.0",
        hostname: "localhost"
      }));
    } else if (msg.type === "get_workflow") {
      ws.send(JSON.stringify({
        type: "get_workflow",
        request_id: msg.request_id,
        output: {
          WorkflowUUID: msg.workflow_id || "wf-sample",
          Status: "SUCCESS",
          WorkflowName: "helloWorkflow",
          ApplicationVersion: "v1.0.0",
          Output: JSON.stringify({ result: "typescript-output" })
        }
      }));
    } else if (msg.type === "list_steps") {
      ws.send(JSON.stringify({
        type: "list_steps",
        request_id: msg.request_id,
        steps: [
          {
            function_id: 1,
            function_name: "step1",
            output: JSON.stringify({ status: "step-done" })
          }
        ]
      }));
    }
  } catch (err) {
    console.error("error handling message:", err);
  }
});

ws.addEventListener("error", (err) => {
  console.error("websocket error:", err);
});
