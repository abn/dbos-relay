---
type: HowTo
---

# Quickstart

This guide walks through running Relay in either zero-dependency embedded mode (using pure-Go SQLite) or clustered production mode (backed by PostgreSQL), minting an API key, running the server, and querying its endpoints.

## Prerequisites

- Go 1.26.7 or later (as required by `go.mod`)
- `curl`
- Optional: Docker or Podman (only if running the local PostgreSQL container)

## Option A: Embedded mode (Zero external dependencies)

For local evaluation, CI runners, edge nodes, or lightweight single-process setups, Relay runs out of the box with embedded SQLite. No database container or installation is required:

### 1. Start Relay in embedded mode

```bash
make build
./bin/relay serve --embedded
```

Relay automatically initializes `./data/relay.db`, applies embedded migrations, and starts the server on `:8090`.

### 2. Mint an API key for embedded Relay

In a separate terminal, create an API key pointing to the embedded database:

```bash
./bin/relay apikey create --embedded --org local --name test-key
```

## Option B: Clustered mode (Flagship PostgreSQL)

For high-availability production clusters, multi-node deployments, or data sovereignty requirements, Relay uses PostgreSQL as its flagship storage engine.

### 1. Start PostgreSQL

Build the Relay binary and start the local PostgreSQL container:

```bash
make build
make db/up
export RELAY_DATABASE_URL="$(make -s db/url)"
```

### 2. Apply migrations

Migrate Relay's control plane schema:

```bash
./bin/relay migrate
```

Relay's `serve` command also applies migrations automatically at startup, making explicit migration optional for single-instance setups. Older server binaries refuse to start against a schema newer than their embedded migration version (`no migration found for version`).

### 3. Mint an API key

Create an initial API key:

```bash
./bin/relay apikey create --org local --name test-key
```

Relay outputs the plaintext key once (prefixed with `dbos_`). Store this key
securely; Relay only persists its SHA-256 hash. In default self-hosted mode
without an external identity provider, Relay resolves requests to the implicit
`local` organization.

### 4. Run the server

Start the Relay server:

```bash
./bin/relay serve
```

The server listens on `:8090` by default. You can configure logging verbosity with `RELAY_LOG_LEVEL` (for example, `debug`, `info`, `warn`, or `error`). Schema migrations run automatically on startup unless `--skip-migrations` or `RELAY_SKIP_MIGRATIONS=true` is set.

## 5. Verify endpoints

Query the health check:

```bash
curl -fsS http://localhost:8090/healthz
```

Inspect the vendored OpenAPI specification:

```bash
curl -fsS http://localhost:8090/openapi.json
```

Open interactive documentation:

```bash
curl -fsS http://localhost:8090/docs
```

Open the web dashboard:

Point your browser to `http://localhost:8090/` to inspect connected executors, workflows, step execution graphs, queues, schedules, and alerts.

## 6. Connecting an executor

A DBOS Transact application connects to Relay over WebSocket using the Conductor configuration. In application code, configure the conductor URL with the `ws://` scheme and provide an API key:

- **Go**:
  ```go
  cfg := dbos.Config{
      ConductorURL:    "ws://localhost:8090",
      ConductorAPIKey: "dbos_...",
  }
  ```
- **Python**:
  ```python
  config = DBOSConfig(conductor_url="ws://localhost:8090", conductor_key="dbos_...")
  ```
- **TypeScript**:
  ```typescript
  const config = {
      conductorURL: "ws://localhost:8090",
      conductorKey: "dbos_...",
  };
  ```

Alternatively, configure the cloud-emulation environment variables (`DBOS__CLOUD=true` together with `DBOS__CONDUCTOR_APP_NAME`, `DBOS__CONDUCTOR_KEY`, and `DBOS__CONDUCTOR_URL`):

```bash
export DBOS__CLOUD="true"
export DBOS__CONDUCTOR_APP_NAME="my-app"
export DBOS__CONDUCTOR_KEY="dbos_..."
export DBOS__CONDUCTOR_URL="ws://localhost:8090"
```

The sample applications in `examples/` wrap these settings using `RELAY_URL` and `RELAY_API_KEY` for local testing convenience.

## 7. Listing executors

Once an application connects, you can query the active executors for that application:

```bash
curl -fsS http://localhost:8090/v2/orgs/local/apps/my-app/executors
```

## 8. Querying workflows and resources

Relay serves the Conductor v2 REST surface. Query workflows for an application:

```bash
# Search workflows
curl -fsS -X POST http://localhost:8090/v2/orgs/local/apps/my-app/workflows/search \
  -H "Content-Type: application/json" \
  -d '{"limit": 10}'

# Inspect a specific workflow
curl -fsS http://localhost:8090/v2/orgs/local/apps/my-app/workflows/{workflowId}

# List workflow execution steps
curl -fsS http://localhost:8090/v2/orgs/local/apps/my-app/workflows/{workflowId}/steps
```

Query application queues and schedules:

```bash
# List queues
curl -fsS http://localhost:8090/v2/orgs/local/apps/my-app/queues

# List schedules
curl -fsS http://localhost:8090/v2/orgs/local/apps/my-app/schedules
```

## 9. Operating with dbosctl

The upstream `dbosctl` CLI works directly with Relay. Point `dbosctl` at Relay's URL:

```bash
export DBOS_URL="http://localhost:8090"
export DBOS_TOKEN="dbos_..."

# List applications
dbosctl app list

# Inspect workflows
dbosctl workflow list --app my-app

# View workflow details
dbosctl workflow get <workflow-id> --app my-app
```

## 10. Error responses

All API errors return RFC 9457 Problem Details (`application/problem+json`):

```json
{
  "type": "about:blank",
  "title": "Service Unavailable",
  "status": 503,
  "detail": "no live executor connected for this application"
}
```

## 11. Upgrades and rollback

When upgrading Relay:
- Schema migrations are additive and applied automatically by `relay serve` or explicitly with `relay migrate`.
- In high-availability deployments, upgrade the database schema before rolling out updated Relay instances.
- Older Relay binaries refuse to start against a newer schema version (`no migration found for version`).
- If a migration fails mid-apply, the database is marked dirty; resolve the schema state before restarting `relay serve`.
- Relay migrations provide paired `.down.sql` scripts, but the CLI exposes no automated rollback command; rollback requires restoring from a database backup or applying down migrations with the external `golang-migrate` tool.
