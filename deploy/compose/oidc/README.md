# Lightweight OIDC Test Stack

This directory provides a lightweight OpenID Connect (OIDC) identity provider
using Dex for local integration testing and continuous integration.

## Architecture

Dex runs as an in-memory identity provider on port 5556, preconfigured with
standard test users, an RSA signing key, and static OAuth 2.0 client
registrations. It enables validating Relay's JWT bearer authentication,
JWKS signature verification, and RFC 8628 Device Authorization Grant flows
without dependencies on external identity platforms.

## Endpoints

* OpenID Discovery: `http://localhost:5556/dex/.well-known/openid-configuration`
* JSON Web Key Set: `http://localhost:5556/dex/keys`
* Token Endpoint: `http://localhost:5556/dex/token`
* Authorization Endpoint: `http://localhost:5556/dex/auth`
* Health Check: `http://localhost:5556/dex/healthz`

## Test Accounts

The following test users are available with password `password`:

| Username | Email | User ID | Description |
|---|---|---|---|
| `admin` | `admin@example.com` | `08a5ba03-8ea8-4647-a33e-14975299fa52` | Global administrator |
| `alice` | `alice@example.com` | `18a5ba03-8ea8-4647-a33e-14975299fa53` | Standard user account |
| `bob` | `bob@acme.corp` | `28a5ba03-8ea8-4647-a33e-14975299fa54` | Domain-claim test account |

## Starting the Stack

Start the container in the background:

```bash
docker compose -f deploy/compose/oidc/docker-compose.yaml up -d
```

Or with Podman:

```bash
podman compose -f deploy/compose/oidc/docker-compose.yaml up -d
```

Verify service availability:

```bash
curl -s http://localhost:5556/dex/.well-known/openid-configuration | jq .
```

To stop the service:

```bash
docker compose -f deploy/compose/oidc/docker-compose.yaml down
```

## Configuring Relay

To connect Relay to this test provider, set the following environment variables:

```bash
export RELAY_AUTH_ENABLED=true
export RELAY_OIDC_ISSUER="http://localhost:5556/dex"
export RELAY_OIDC_AUDIENCE="relay-client"
```
