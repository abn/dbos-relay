import os
from dbos import DBOS, SetWorkflowConfig

# Configure conductor URL and API key from environment
# DBOS Transact connects to Relay's /websocket/{appName}/{conductorKey}
app_name = os.environ.get("DBOS_APP_NAME", "sample-app")
conductor_url = os.environ.get("RELAY_URL", "http://localhost:8090")
api_key = os.environ.get("RELAY_API_KEY", "")

@DBOS.workflow()
def hello_workflow(name: str) -> str:
    return f"Hello, {name}!"

if __name__ == "__main__":
    DBOS.launch()
