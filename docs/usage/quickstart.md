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
