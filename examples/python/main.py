import json
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import psycopg
from dbos import DBOS, DBOSConfig

app_name = os.environ.get("DBOS_APP_NAME", "python-sample-app")
relay_url = os.environ.get("RELAY_URL", "http://localhost:8090")
api_key = os.environ.get("RELAY_API_KEY", "")
db_url = os.environ.get("DBOS_SYSTEM_DATABASE_URL", "postgres://relay:relay@postgres:5432/relay?sslmode=disable")
role = os.environ.get("ROLE", "")

def record_step_execution(workflow_id: str, step_name: str) -> None:
    clean_url = db_url.replace("+psycopg", "")
    with psycopg.connect(clean_url) as conn:
        with conn.cursor() as cur:
            cur.execute("""
                CREATE TABLE IF NOT EXISTS test_step_executions (
                    workflow_id TEXT NOT NULL,
                    step_name TEXT NOT NULL,
                    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
                );
                INSERT INTO test_step_executions (workflow_id, step_name, executed_at)
                VALUES (%s, %s, NOW());
            """, (workflow_id, step_name))
        conn.commit()

# Configure DBOS SDK
db_sa_url = db_url
if db_sa_url.startswith("postgres://"):
    db_sa_url = db_sa_url.replace("postgres://", "postgresql+psycopg://", 1)
elif db_sa_url.startswith("postgresql://") and not db_sa_url.startswith("postgresql+"):
    db_sa_url = db_sa_url.replace("postgresql://", "postgresql+psycopg://", 1)

cfg: DBOSConfig = {
    "name": app_name,
    "system_database_url": db_sa_url,
    "conductor_url": relay_url,
    "conductor_key": api_key,
}

DBOS(config=cfg)

@DBOS.step()
def step1(order_id: str) -> str:
    record_step_execution(DBOS.workflow_id, "step1")
    return "step1-completed"

@DBOS.step()
def step2(order_id: str) -> str:
    record_step_execution(DBOS.workflow_id, "step2")
    return "step2-completed"

@DBOS.workflow()
def order_workflow(order_id: str) -> str:
    step1(order_id)
    if role == "victim":
        # Sleep until killed in chaos cell
        time.sleep(1800)
    step2(order_id)
    return f"order-{order_id}-completed"

class TriggerHandler(BaseHTTPRequestHandler):
    def do_POST(self) -> None:
        self._handle()

    def do_GET(self) -> None:
        self._handle()

    def _handle(self) -> None:
        if self.path.startswith("/trigger"):
            handle = DBOS.start_workflow(order_workflow, "python-order")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"workflow_id": handle.workflow_id}).encode("utf-8"))
        elif self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Type", "text/plain")
            self.end_headers()
            self.wfile.write(b"OK")
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, format: str, *args: object) -> None:
        pass

def start_http_server() -> None:
    port = int(os.environ.get("HTTP_PORT", "8081"))
    server = ThreadingHTTPServer(("0.0.0.0", port), TriggerHandler)
    server.serve_forever()

if __name__ == "__main__":
    if role == "survivor":
        time.sleep(3)

    http_thread = threading.Thread(target=start_http_server, daemon=True)
    http_thread.start()

    DBOS.launch()
    print(f"DBOS Python sample application launched successfully for app {app_name}", flush=True)

    while True:
        time.sleep(1)
