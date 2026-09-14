# DBOS Relay Go Sample

This is a minimal DBOS Transact Go workflow application that connects to a DBOS Relay instance.

## Running the Sample

1. Build the application binary:
   ```bash
   go build -o bin/app .
   ```

2. Export the required environment variables:
   ```bash
   export DBOS_APP_NAME="golang-sample-app"
   export DBOS_SYSTEM_DATABASE_URL="postgres://relay:relay@localhost:5433/relay_golang?sslmode=disable"
   export RELAY_URL="http://localhost:8090"
   export RELAY_API_KEY="your-api-key"
   export HTTP_PORT="8080"
   ```

3. Launch the application:
   ```bash
   ./bin/app
   ```

4. Trigger a workflow:
   ```bash
   curl -X POST http://localhost:8080/trigger
   ```
