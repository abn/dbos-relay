# DBOS Relay TypeScript Sample

This is a minimal DBOS Transact TypeScript workflow application that connects to a DBOS Relay instance.

## Running the Sample

1. Install the dependencies:
   ```bash
   npm install
   ```

2. Export the required environment variables:
   ```bash
   export DBOS_APP_NAME="sample-app"
   export DBOS_SYSTEM_DATABASE_URL="postgres://relay:relay@localhost:5432/relay?sslmode=disable"
   export RELAY_URL="http://localhost:8090"
   export RELAY_API_KEY="your-api-key"
   ```

3. Build and launch the application:
   ```bash
   npm run build
   npm start
   ```

4. Trigger a workflow:
   ```bash
   curl -X POST http://localhost:8082/trigger
   ```
