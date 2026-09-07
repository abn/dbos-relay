---
type: Reference
title: Components
description: The module boundaries and the data each one owns.
status: draft
---

# Components

This page describes an intended design. No product code exists yet, so the
module names are language-agnostic and get rewritten against the code once it
lands.

Relay is one process with a small number of internal boundaries. The
boundaries exist so that the parts most likely to change, the wire protocol
and the HTTP contract, can change without dragging the rest with them.

## The shape

Executors connect outbound over WebSockets. Clients, the dashboard, and a
metrics scraper connect inbound over HTTP. Between them sit a connection hub
and a router; behind them sits Relay's own small Postgres database.

```
  executor --ws--\                                  /--http-- control client
  executor --ws----  gateway -> hub -> router -> api --http-- dashboard
  executor --ws--/            (correlation) (local  \--http-- metrics scrape
                                            or peer)
                                |            |         |
                          liveness and    ownership   store
                          recovery        lookup
                                |            |         |
                                \------ Relay Postgres ------/
```

## Modules

**Protocol.** Types and a codec for the executor WebSocket protocol, with no
input or output of its own. Written from the protocol specification rather
than lifted from an SDK, and versioned.

**Hub.** Accepts connections, authenticates them, registers the executor, and
multiplexes requests and responses over each connection by request identifier.
It handles pings, deadlines, out-of-order replies, and unsolicited messages.

**Liveness.** The executor state machine and its timers, plus recovery
dispatch and confirmation. See [recovery](recovery.md).

**Router.** Given an organisation and an application, picks a healthy
executor. If that executor's connection is owned by another instance, the
request is forwarded to that instance over HTTP with a signed internal header.

**API.** The HTTP server, generated from the vendored OpenAPI document. The
handlers are deliberately thin: validate, authorise, then either read the
store or dispatch through the router. No hand-written JSON, so the contract
cannot drift from the specification.

**Store.** Relay's own schema, migrations, and typed queries. It holds
organisations, applications, API keys, executors, instances, alert rules,
audit entries, and metric samples. It holds no workflow data.

**Auth.** API keys, hashed at rest and scoped to applications and
permissions, and OIDC token validation for the operations that require it.

**Metrics.** An OpenMetrics endpoint over executor and request counters.

**Dashboard.** The web interface, talking only to the HTTP API. It has no
database access, ever. It is not called the console: that is the vendor's
product name, and the trademark rule reaches module names.

## Rules the boundaries encode

- Relay's database is small and its own. Workflow data is fetched from
  executors per request and is not cached.
- Every executor-served request carries a deadline, and failure maps onto the
  status codes the published specification defines.
- One executor connection has exactly one owning instance. Ownership and its
  lease live in the database, so an instance that dies releases its executors
  by lease expiry rather than by cleanup.
- Correlation is by request identifier, and the hub tolerates messages it did
  not ask for.
