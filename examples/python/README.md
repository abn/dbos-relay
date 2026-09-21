# Relay Python Sample

This is a minimal DBOS Transact Python workflow application that connects to a Relay instance.
Dependencies are declared directly inside `main.py` via inline script metadata (PEP 723).

## Running the Sample

1. Export the required environment variables:
   ```bash
   export DBOS_APP_NAME="sample-app"
   export DBOS_SYSTEM_DATABASE_URL="postgres://relay:relay@localhost:5432/relay?sslmode=disable"
   export RELAY_URL="http://localhost:8090"
   export RELAY_API_KEY="your-api-key"
   ```

2. Launch the application with `uv`:
   ```bash
   uv run main.py
   ```

   Alternatively, with standard `pip` (pinned to match `main.py`):
   ```bash
   pip install "dbos==3.0.0" "psycopg-binary>=3.3.0"
   python main.py
   ```

4. Trigger a workflow:
   ```bash
   curl -X POST http://localhost:8081/trigger
   ```
