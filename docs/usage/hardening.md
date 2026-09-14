---
type: HowTo
---

# Production Hardening

Operational guidance for securing Relay in production deployments.

## TLS termination and reverse proxies

Relay can terminate TLS directly or run behind a fronting reverse proxy (such as Envoy, NGINX, Traefik, or a Kubernetes Ingress Controller).

### In-process TLS

To terminate TLS directly in Relay, configure `RELAY_TLS_CERT_FILE` and `RELAY_TLS_KEY_FILE`:

```bash
export RELAY_TLS_CERT_FILE=/path/to/server.crt
export RELAY_TLS_KEY_FILE=/path/to/server.key
./bin/relay serve
```

Both variables must be set together. When configured, Relay binds an HTTPS listener on `RELAY_LISTEN_ADDR`.

### Reverse proxy deployment and URI logging warning

When deploying Relay behind a TLS-terminating reverse proxy, take special care with request logging:

> [!WARNING]
> Reverse proxies fronting Relay must not log the request URI or request path for WebSocket connections.
> In the Conductor executor protocol, the executor credentials (conductor API key) are transmitted in the WebSocket path (`/websocket/{appName}/{conductorKey}`). Logging full request URIs will leak sensitive credentials into proxy access logs.

Configure your proxy access logs to log request paths only for standard HTTP REST paths, or strip path arguments for `/websocket/*`.

## Peer forwarding across Relay instances

In multi-instance deployments, Relay instances forward requests across instances when an application executor is connected to a peer.

- By default, peer forwarding dials `http://` against the peer instance's advertise address and port.
- When instances run behind TLS or use in-process TLS certificates, configure `RELAY_PEER_SCHEME=https` or supply an `https://` prefix in `RELAY_ADVERTISE_ADDRESS`.
- Instance-to-instance forwards are authenticated using HMAC-SHA256 signatures via `RELAY_INTERNAL_SECRET`. Ensure `RELAY_INTERNAL_SECRET` is set to a strong random key in multi-node deployments.
