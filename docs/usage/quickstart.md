---
type: HowTo
---

# Quickstart

This guide walks through starting a local PostgreSQL database, migrating
the Relay schema, minting an API key, running the Relay server, and querying
its unauthenticated endpoints.

## Prerequisites

- Go 1.24 or later
- Docker or Podman (for running the local database container)
- `curl`

## 1. Start the database

Start the local PostgreSQL container:

```bash
make db/up
export RELAY_DATABASE_URL="$(make -s db/url)"
```

## 2. Apply migrations

Migrate Relay's control plane schema:

```bash
./bin/relay migrate
```

## 3. Mint an API key

Create an initial API key:

```bash
./bin/relay apikey create --org acme --name test-key
```

Relay outputs the plaintext key once (prefixed with `dbos_`). Store this key
securely; Relay only persists its SHA-256 hash.

## 4. Run the server

Start the Relay server:

```bash
./bin/relay serve
```

The server listens on `:8090` by default.

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

## 6. Connecting an executor

A DBOS Transact application connects to Relay by configuring the `RELAY_URL` and `RELAY_API_KEY` environment variables.

```bash
export RELAY_URL="http://localhost:8090"
export RELAY_API_KEY="dbos_..."
```

## 7. Listing executors

Once an application connects, you can query the active executors for that application:

```bash
curl -fsS http://localhost:8090/v2/orgs/acme/apps/my-app/executors
```

## 8. Querying workflows and resources

Relay serves the Conductor v2 REST surface. Query workflows for an application:

```bash
# Search workflows
curl -fsS -X POST http://localhost:8090/v2/orgs/acme/apps/my-app/workflows/search \
  -H "Content-Type: application/json" \
  -d '{"limit": 10}'

# Inspect a specific workflow
curl -fsS http://localhost:8090/v2/orgs/acme/apps/my-app/workflows/{workflowId}

# List workflow execution steps
curl -fsS http://localhost:8090/v2/orgs/acme/apps/my-app/workflows/{workflowId}/steps
```

Query application queues and schedules:

```bash
# List queues
curl -fsS http://localhost:8090/v2/orgs/acme/apps/my-app/queues

# List schedules
curl -fsS http://localhost:8090/v2/orgs/acme/apps/my-app/schedules
```

## 9. Operating with dbosctl

The upstream `dbosctl` CLI works directly with Relay. Point `dbosctl` at Relay's URL:

```bash
export DBOS_CONDUCTOR_URL="http://localhost:8090"
export DBOS_API_KEY="dbos_..."

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
