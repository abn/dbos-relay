# Dual-Instance Scale Deployment

This deployment runs a horizontally scaled Relay cluster with two nodes fronted by an Nginx reverse proxy, backed by a shared PostgreSQL database.

## Architecture

```
                    +-----------------------------+
                    |        Reverse Proxy        |
                    |         (Port 8090)         |
                    +--------------+--------------+
                                   |
                +------------------+------------------+
                |                                     |
                v                                     v
+-------------------------------+   +-------------------------------+
|         relay-node-1          |   |         relay-node-2          |
|          (Port 8091)          |<->|          (Port 8092)          |
+---------------+---------------+   +---------------+---------------+
                |                                   |
                +-----------------+-----------------+
                                  |
                                  v
                  +-------------------------------+
                  |          PostgreSQL           |
                  |          (Port 5432)          |
                  +-------------------------------+
```

The cluster components:
1. `proxy` (port 8090): An Nginx reverse proxy load balancing WebSocket connections (`/websocket/`) and HTTP API traffic (`/v2/`, `/v1/metrics`, `/healthz`). It explicitly blocks external access to private `/internal/` forwarding endpoints.
2. `relay-node-1` (port 8091): First control plane instance with advertise address `relay-node-1`.
3. `relay-node-2` (port 8092): Second control plane instance with advertise address `relay-node-2`.
4. `postgres`: Shared database storing applications, API keys, executor registrations, instance records, and leases.

## Dual-Instance Operation

Each Relay instance registers its presence in the shared database upon boot:
- The instance generates a unique identifier and records its advertised address and port in the `instances` table.
- A background heartbeat loop regularly updates the instance `heartbeat_at` timestamp.
- Nodes track other active instances in the cluster by querying the `instances` table.

## Executor Leases and Ownership

When an application executor connects via WebSocket (either directly or through the proxy):
- The receiving node handshakes with the executor and upserts an entry into the `executors` table.
- The record designates `owner_instance_id` to the receiving node and sets an active `lease_expires_at` timestamp.
- As long as the WebSocket connection remains healthy, periodic ping and pong exchanges touch the lease expiration timestamp, renewing ownership.

## Peer Forwarding

When an HTTP API request arrives at a node that does not own the target application's active executor:
1. The receiving node queries the store to locate connected executors for the application.
2. It identifies the remote instance owning the executor via `owner_instance_id`.
3. It fetches the remote instance's advertise address and port from the `instances` table.
4. It prepares an internal forward request directed to `http://<peer-address>:<peer-port>/internal/v1/forward/<application-id>`.
5. It computes an HMAC signature using `RELAY_INTERNAL_SECRET` and attaches security headers:
   - `X-Relay-Forward-Signature`: Hex-encoded HMAC-SHA256 signature.
   - `X-Relay-Forward-Timestamp`: Request timestamp for drift detection.
   - `X-Relay-Forward-Hop`: Hop count for loop prevention (must be 0).
   - `X-Relay-Forward-Nonce`: Cryptographic nonce preventing replay attacks.
   - `X-Relay-Forward-Deadline`: Context deadline propagation.
6. The owning peer verifies the signature, nonce, and hop count, executes the request against its local executor WebSocket, and returns the response.
7. The receiving node relays the response back to the client.

## Disaster Recovery and Lease Reassignment

If an instance crashes or becomes unresponsive:
1. Heartbeat timestamps in `instances` cease updating.
2. Executor leases owned by the failed instance expire once `lease_expires_at` passes.
3. Surviving nodes periodically run `AdoptExpiredExecutors` (configured via `AdoptionInterval`).
4. A surviving node adopts the orphaned executor records by updating `owner_instance_id` to itself and refreshing the lease.
5. The adopting node's liveness tracker enters a grace period for the disconnected executor.
6. When the executor client detects the dropped WebSocket connection, its reconnection logic reconnects to the proxy, which routes it to a surviving healthy node.
7. Upon reconnection, the new node assumes local management and clears any pending grace timer.

## Starting the Cluster

Prerequisites:
- Pre-built Relay binary in `bin/relay` (`make build`).

Start the cluster:
```bash
docker compose -f deploy/compose/scale/docker-compose.yml up -d
```

Verify services:
```bash
# Check health of proxy
curl http://127.0.0.1:8090/healthz

# Check health of node 1 directly
curl http://127.0.0.1:8091/healthz

# Check health of node 2 directly
curl http://127.0.0.1:8092/healthz

# Scrape metrics through proxy
curl http://127.0.0.1:8090/v1/metrics
```

Stop the cluster:
```bash
docker compose -f deploy/compose/scale/docker-compose.yml down -v
```
