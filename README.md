# Relay

[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/abn/dbos-relay)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

An open-source control plane for DBOS Transact applications, compatible with
the Conductor executor protocol and HTTP API. Self-hostable, MIT licensed, and
not affiliated with or endorsed by DBOS, Inc.

DBOS Transact applications persist their workflow execution state in their own
application database and recover workflows locally when the owning process
restarts. Distributed cross-node recovery, fleet monitoring, centralized
telemetry, step DAG visualizations, and scheduled executions rely on a control
plane. Relay is that control plane, engineered in the open under the MIT
license.

Unmodified DBOS applications connect to Relay simply by pointing their Conductor
URL and API key at the Relay instance.

## Key Features

- **Dual-Engine Storage (ADR 0011)**: Pure-Go embedded SQLite (`modernc.org/sqlite`) for zero-dependency local development and single-node instances, plus PostgreSQL for clustered, multi-instance production deployments.
- **Full Conductor Protocol Parity**: Complete 32-message bidirectional WebSocket protocol support, verified across all four official DBOS Transact SDKs (TypeScript, Python, Go, and Java).
- **Zero Application Changes**: Requires no upstream SDK modifications or custom wrappers; applications configure standard DBOS environment variables.
- **Embedded Web Dashboard**: Self-hosted operations console on port 8090 with interactive SVG workflow DAG family visualization, execution curves, canvas viewport (pan/zoom), step history, real-time Server-Sent Events (SSE) telemetry, schedule management, and alerting rules.
- **Declarative Fleet Management**: Manage applications, queues, and alert policies declaratively via `relay.yaml` with `relay apply` and `relay diff`.
- **High Availability & Lease Fencing**: Multi-node coordination with distributed leases, heartbeat tracking, automatic orphan reconciliation, and peer proxy forwarding.
- **Clean-Room Derivation**: Clean-room implementation derived solely from permitted public specifications and observable client contracts without proprietary artifacts.

## Quickstart

### 1. Standalone Embedded Mode (Zero Dependencies)

Run Relay with pure-Go embedded SQLite storage (no PostgreSQL or Docker required):

```bash
# Download the binary
curl -fsSL https://github.com/abn/relay/releases/latest/download/relay-linux-amd64 -o relay
chmod +x relay

# Start Relay in embedded mode
./relay serve --embedded

# Open http://localhost:8090 in your browser to access the dashboard
```

### 2. Containerized with Podman or Docker

```bash
# Run standalone embedded container
podman run -d \
  --name relay \
  -p 8090:8090 \
  ghcr.io/abn/relay:latest serve --embedded
```

Or connect Relay to an external PostgreSQL instance:

```bash
podman run -d \
  --name relay \
  -p 8090:8090 \
  -e RELAY_DATABASE_URL="postgres://user:pass@postgres:5432/relay?sslmode=disable" \
  ghcr.io/abn/relay:latest serve
```

### 3. Connect DBOS Applications

Point your DBOS Transact applications to Relay using standard configuration:

```bash
# Python
export DBOS_APP_NAME="orders-service"
export RELAY_URL="ws://localhost:8090"
export RELAY_API_KEY="your-api-key"
python3 main.py

# TypeScript
export DBOS_APP_NAME="orders-service"
export RELAY_URL="ws://localhost:8090"
export RELAY_API_KEY="your-api-key"
npm start

# Go
export DBOS_APP_NAME="orders-service"
export RELAY_URL="ws://localhost:8090"
export RELAY_API_KEY="your-api-key"
go run .
```

## Documentation

The project wiki is maintained in [`docs/`](docs/index.md) as an Open Knowledge Format (OKF v0.2) bundle:

- [Overview](docs/overview.md): Project scope, architecture, and background
- [Design & Compatibility Tiers](docs/design/compatibility-tiers.md): Verification tiers and conformance gates
- [Architecture](docs/architecture/index.md): Module boundaries, recovery lifecycle, and data-plane access
- [Executor WebSocket Protocol](docs/protocol/executor-ws.md): Wire specification and frame formats
- [Architecture Decision Records (ADRs)](docs/adr/index.md): Technical decisions and design rationale
- [Software Changelog](docs/changelog.md): Release history managed by Release Please
- [Documentation Update Log](docs/log.md): Evolution and provenance log of the knowledge base
- [Clean-Room Rules](docs/contribution/clean-room.md): Permitted sources and contributor rules

## Verification and Testing

Relay enforces automated conformance and quality gates across all supported SDKs:

```bash
# Run full quality gate (lint, vet, unit tests, code generation drift)
make check

# Run end-to-end multi-SDK conformance matrix (Python, TypeScript, Go, Java)
make verify-sdk

# Run live database conformance battery against PostgreSQL
RELAY_TEST_DATABASE_URL="postgres://relay:relay@localhost:5433/relay?sslmode=disable" make verify-live
```

## Contributing

Contributions are welcome. Please read the [contributor guide](docs/contribution/guide.md), the [clean-room rules](docs/contribution/clean-room.md), and [AGENTS.md](AGENTS.md) before submitting pull requests.

## License

MIT. See [LICENSE](LICENSE).
