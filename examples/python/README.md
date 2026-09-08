# DBOS Relay Python Sample

This is a minimal DBOS Transact Python workflow application that connects to a DBOS Relay instance.

## Running the Sample

1. Create a virtual environment and install the requirements:
   ```bash
   python -m venv .venv
   source .venv/bin/activate
   pip install -r requirements.txt
   ```

2. Export the required environment variables:
   ```bash
   export DBOS_APP_NAME="sample-app"
   export RELAY_URL="http://localhost:8090"
   export RELAY_API_KEY="your-api-key"
   ```

3. Launch the application:
   ```bash
   python main.py
   ```
