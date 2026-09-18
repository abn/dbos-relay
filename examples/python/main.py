# /// script
# requires-python = ">=3.10"
# dependencies = [
#     "dbos>=2.31.0",
#     "psycopg-binary>=3.3.0",
# ]
# ///

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
chaos_sleep_secs = int(os.environ.get("CHAOS_SLEEP_SECS", "0"))

def record_step_execution(workflow_id: str, step_name: str) -> None:
    clean_url = db_url.replace("+psycopg", "")
    last_exc = None
    for attempt in range(1, 11):
        try:
            with psycopg.connect(clean_url) as conn:
                with conn.cursor() as cur:
                    cur.execute("""
                        INSERT INTO test_step_executions (workflow_id, step_name, executed_at)
                        VALUES (%s, %s, NOW());
                    """, (workflow_id, step_name))
                conn.commit()
            return
        except Exception as exc:
            last_exc = exc
            time.sleep(0.5)
    print(f"record_step_execution failed for {step_name} ({workflow_id}): {last_exc}", file=sys.stderr, flush=True)

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
    if chaos_sleep_secs > 0 and role == "primary":
        time.sleep(chaos_sleep_secs)
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
        elif self.path.startswith("/fork"):
            from urllib.parse import parse_qs, urlparse
            qs = parse_qs(urlparse(self.path).query)
            orig_id = qs.get("original_workflow_id", [""])[0]
            if not orig_id:
                self.send_response(400)
                self.end_headers()
                return
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
    if role == "secondary":
        time.sleep(3)

    http_thread = threading.Thread(target=start_http_server, daemon=True)
    http_thread.start()

    launched = False
    for attempt in range(1, 16):
        try:
            DBOS.launch()
            launched = True
            break
        except Exception as exc:
            print(f"Attempt {attempt}/15 to launch DBOS failed: {exc}. Retrying in 1s...", file=sys.stderr, flush=True)
            time.sleep(1)
    if not launched:
        sys.exit("Failed to launch DBOS after 15 attempts")

    print(f"DBOS Python sample application launched successfully for app {app_name}", flush=True)

    while True:
        time.sleep(1)
