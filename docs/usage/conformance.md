---
type: HowTo
---

# Conformance testing

Relay includes an automated conformance test suite that certifies whether a target
server adheres to the DBOS Conductor protocol and REST specification. The suite can
run either in-process during local testing or as a blackbox client against any running
Conductor or Relay endpoint. Note that the conformance suite utilizes a synthetic,
fake executor peer to simulate SDK connectivity during the tests.

> [!WARNING]
> The conformance suite writes state to the target server and is intended for scratch
> or staging environments only. A full suite run will register six executors, create an
> application row if the app name is new, create an alerting rule in battery 7 (cleaned
> up in a subsequent check), and route wire mutations to connected executors. Running
> against non-local targets requires the `--allow-destructive` safety flag.

## Batteries tested

The conformance suite evaluates 8 distinct batteries:

1. **Specification & System Probes**: Validates `/healthz`, `/openapi.json`,
   `/openapi-3.0.json`, `/openapi.yaml`, `/docs`, and Prometheus `/v1/metrics`.
2. **WebSocket Handshake & Fleet Registration**: Verifies that invalid API keys are
   rejected with HTTP 401 Problem Details, valid keys complete the WebSocket upgrade
   at `/websocket/{appName}/{conductorKey}`, and the executor appears with `HEALTHY`
   status in the fleet list.
3. **REST & Wire Multiplexing (Observability)**: Tests dispatching queries for
   workflows, workflow details, and step executions over WebSockets.
4. **Workflow Control Operations**: Verifies workflow mutations (`cancel`, `resume`,
   and `restart`).
5. **Queues & Schedules Operations**: Verifies listing and pausing/resuming queues
   and schedules.
6. **Workflow Recovery & Liveness Lifecycle**: Tests disconnect detection and
   replacement executor adoption readiness.
7. **Alerting Rules Management**: Validates creating, listing, and deleting
   alerting rules.
8. **RFC 9457 Problem Details & Identity Gating**: Verifies that all client errors
   return `application/problem+json` matching the OpenAPI `ErrorModel` schema, and that
   OAuth-gated routes return HTTP 404 Problem Details when running in self-hosted
   no-auth mode.

## Running the conformance test suite

### Via the Relay CLI

You can run the conformance suite directly from the compiled binary against a running
server:

```bash
./bin/relay test-conformance --target http://localhost:8090 --key "$RELAY_API_KEY"
```

To generate a Markdown scorecard report file:

```bash
./bin/relay test-conformance --target http://localhost:8090 --key "$RELAY_API_KEY" --report scorecard.md
```

### Via Make and Go test

To run the suite in an automated test environment:

```bash
make test/conformance
```

When `RELAY_CONFORMANCE_TARGET` is set in the environment, the tests run against that
endpoint:

```bash
export RELAY_CONFORMANCE_TARGET="http://localhost:8090"
export RELAY_CONFORMANCE_KEY="$RELAY_API_KEY"
make test/conformance
```

When unset, the suite spins up an in-process Relay server and Postgres store to execute
the batteries locally.
