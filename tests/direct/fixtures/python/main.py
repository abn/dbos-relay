import json
import os
import threading
import time

from dbos import DBOS, DBOSPortableJSONSerializer

app_name = os.environ["DBOS_APP_NAME"]
system_database_url = os.environ["DBOS_SYSTEM_DATABASE_URL"]
executor_id = os.environ.get("DBOS__VMID", "python-direct-fixture")

DBOS(
    config={
        "name": app_name,
        "system_database_url": system_database_url,
        "executor_id": executor_id,
        "serializer": DBOSPortableJSONSerializer(),
    }
)


@DBOS.step()
def fixture_step() -> str:
    return "step-ok"


@DBOS.workflow()
def fixture_workflow(name: str) -> str:
    fixture_step()
    return f"hello {name}"


blocking_started = threading.Event()


@DBOS.step()
def checkpoint_step() -> str:
    return "checkpoint"


@DBOS.step()
def blocking_start_step() -> str:
    blocking_started.set()
    return "started"


@DBOS.workflow()
def blocking_workflow() -> str:
    checkpoint_step()
    blocking_start_step()
    time.sleep(30)
    return "done"


DBOS.launch()
completed = DBOS.start_workflow(fixture_workflow, "python")
completed.get_result()
blocking = DBOS.start_workflow(blocking_workflow)
if not blocking_started.wait(timeout=10):
    raise RuntimeError("blocking workflow did not start")
print(
    json.dumps(
        {
            "completed_workflow_id": completed.workflow_id,
            "blocking_workflow_id": blocking.workflow_id,
            "executor_id": executor_id,
        }
    ),
    flush=True,
)
os._exit(0)
