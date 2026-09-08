import { DBOS } from "@dbos-inc/dbos-sdk";
import { Client } from "pg";
import * as http from "node:http";

const dbURL = process.env.DBOS_SYSTEM_DATABASE_URL || "postgres://relay:relay@postgres:5432/relay?sslmode=disable";
const appName = process.env.DBOS_APP_NAME || "typescript-sample-app";
const relayURL = process.env.RELAY_URL || "ws://relay:8090";
const apiKey = process.env.RELAY_API_KEY || "";
const role = process.env.ROLE || "";
const httpPort = parseInt(process.env.HTTP_PORT || "8082", 10);

async function recordStepExecution(workflowID: string, stepName: string): Promise<void> {
  const client = new Client({ connectionString: dbURL });
  await client.connect();
  try {
    await client.query(`
      CREATE TABLE IF NOT EXISTS test_step_executions (
        workflow_id TEXT NOT NULL,
        step_name TEXT NOT NULL,
        executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
      );
    `);
    await client.query(`
      INSERT INTO test_step_executions (workflow_id, step_name, executed_at)
      VALUES ($1, $2, NOW());
    `, [workflowID, stepName]);
  } finally {
    await client.end();
  }
}

export class SampleApp {
  @DBOS.step()
  static async step1(orderID: string): Promise<string> {
    const wfID = DBOS.workflowID || "unknown";
    await recordStepExecution(wfID, "step1");
    return "step1-completed";
  }

  @DBOS.step()
  static async step2(orderID: string): Promise<string> {
    const wfID = DBOS.workflowID || "unknown";
    await recordStepExecution(wfID, "step2");
    return "step2-completed";
  }

  @DBOS.workflow()
  static async orderWorkflow(orderID: string): Promise<string> {
    await SampleApp.step1(orderID);
    if (role === "primary") {
      // Sleep until killed in chaos cell
      await new Promise((resolve) => setTimeout(resolve, 1800000));
    }
    await SampleApp.step2(orderID);
    return `order-${orderID}-completed`;
  }
}

function startHttpServer(): void {
  const server = http.createServer(async (req, res) => {
    if (req.url?.startsWith("/trigger")) {
      try {
        const handle = await DBOS.startWorkflow(SampleApp.orderWorkflow)("ts-order");
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ workflow_id: handle.workflowID }));
      } catch (err) {
        res.writeHead(500, { "Content-Type": "text/plain" });
        res.end(String(err));
      }
    } else if (req.url?.startsWith("/fork")) {
      try {
        const u = new URL(req.url, `http://${req.headers.host || "localhost"}`);
        const origId = u.searchParams.get("original_workflow_id");
        if (!origId) {
          res.writeHead(400, { "Content-Type": "text/plain" });
          res.end("missing original_workflow_id");
          return;
        }
        const handle = await DBOS.forkWorkflow(origId, 0);
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ workflow_id: handle.workflowID }));
      } catch (err) {
        res.writeHead(500, { "Content-Type": "text/plain" });
        res.end(String(err));
      }
    } else if (req.url === "/health") {
      res.writeHead(200, { "Content-Type": "text/plain" });
      res.end("OK");
    } else {
      res.writeHead(404);
      res.end();
    }
  });

  server.listen(httpPort, "0.0.0.0");
}

async function main(): Promise<void> {
  if (role === "secondary") {
    await new Promise((resolve) => setTimeout(resolve, 3000));
  }

  startHttpServer();

  await DBOS.launch({
    conductorKey: apiKey,
    conductorURL: relayURL,
  });

  console.log(`DBOS TypeScript sample application launched successfully for app ${appName}`);
}

main().catch((err) => {
  console.error("Failed to launch DBOS TypeScript sample app:", err);
  process.exit(1);
});
