# DBOS Relay Java Sample

This is a minimal DBOS Transact Java workflow application that connects to a DBOS Relay instance.

## Running the Sample

1. Build the shaded application JAR using Maven:
   ```bash
   mvn clean package -DskipTests
   ```
   This generates `target/dbos-java-sample-1.0.0.jar`.

2. Export the required environment variables:
   ```bash
   export DBOS_APP_NAME="java-sample-app"
   export DBOS_SYSTEM_DATABASE_URL="postgres://relay:relay@localhost:5433/relay_java?sslmode=disable"
   export RELAY_URL="http://localhost:8090"
   export RELAY_API_KEY="your-api-key"
   ```

3. Launch the application:
   ```bash
   java -jar target/dbos-java-sample-1.0.0.jar
   ```

4. Trigger a workflow:
   ```bash
   curl -X POST http://localhost:8083/trigger
   ```
