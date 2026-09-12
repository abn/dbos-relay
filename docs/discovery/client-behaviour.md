---
type: Reference
title: Upstream client behaviour and conformance checklist
description: Operational inventory of dbosctl commands, HTTP mappings, no-auth semantics, device authorization, and conformance checklist.
status: draft
---

# Upstream client behaviour and conformance checklist

This document records the operational behaviour of the upstream command-line
client, `dbosctl`, derived under clean-room rules from its public source code
and vendored OpenAPI specifications. It serves as the primary behavioral
contract and acceptance checklist for Relay's REST engine and
authentication layer.

## Provenance and clean-room citation

All findings in this document are derived from the following permitted source:
* Repository: `https://github.com/dbos-inc/dbos-ctl`
* Pinned Commit: `9d14ed3f0ccddb84cd3390e0bddbcfb9ea9a32a6`
* Date Confirmed: 2026-09-08

## 1. License analysis

Inspection of `LICENSE` and `go.mod` in the repository root confirms:
* **License Type**: MIT License (`MIT`).
* **Copyright**: Copyright (c) 2026 DBOS, Inc.
* **Go Module**: `github.com/dbos-inc/dbos-ctl`
* **Terms**: Standard permissive terms granting free permission to deal in the
  software without restriction, including rights to use, copy, modify, merge,
  publish, distribute, sublicense, and sell copies, subject only to retaining the
  copyright notice and disclaimer.
* **Compatibility Verdict**: Fully compatible with Relay's clean-room derivation
  rules. The client source is permissively licensed and verified.

## 2. Profile and URL resolution mechanics

The client reads configuration and credentials from the user's home directory:
* Config file: `$XDG_CONFIG_HOME/dbos/config.yaml` (or `~/.config/dbos/config.yaml`).
* Credentials file: `$XDG_CONFIG_HOME/dbos/credentials.yaml`.

Configuration uses a strict precedence chain:
`command-line flag > environment variable > active profile`

Resolution behaves as follows (`internal/config/resolve.go`, `internal/cli/common.go`):

1. **Active Profile**:
   * `--profile <name>` > `DBOS_PROFILE` > `current` field in `config.yaml`.
2. **Conductor Base URL**:
   * `--url <url>` > `DBOS_URL` > profile `url` > derived managed domain URL.
   * If the profile has `domain` set (or resolved URL host matches `cloud.dbos.dev`),
     the URL is derived as `https://{domain}/conductor`. Cleartext HTTP is
     rejected for managed production.
3. **Organization**:
   * `--org <org>` > `DBOS_ORG` > profile `org` > stored login org in
     `credentials.yaml` > live lookup via `GET /v2/users/me` (for ad-hoc tokens)
     > default `"local"` (when `auth: none`).
4. **Application**:
   * `-a` / `--app <app>` > `DBOS_APP` > profile `app`.
   * For app-scoped commands (`workflow`, `queue`, `schedule`), failing to resolve
     an application name returns an immediate error before sending a request.
5. **Bearer Token**:
   * `DBOS_TOKEN` > stored login `token` for profile in `credentials.yaml`.
   * Sent as header: `Authorization: Bearer <token>`.
   * If `auth: none` (or target is unauthenticated self-hosted), no header is sent.
   * If token starts with prefix `dbos_`, it is treated as a static API key and
     never refreshed.
   * If token does not start with `dbos_` and expires, the client refreshes it
     using the stored refresh token before executing the request.
6. **Output Format**:
   * `-o` / `--output <format>` (`table` default, `json`, and for specific commands
     `ids`). Not stored in config or environment.

## 3. No-auth mode behaviour

A self-hosted deployment running without authentication (`auth: none`) operates
under specific client and server conventions.

### Client-side handling

* **Hardcoded Organization**: When `auth` is `none` and no organization is
  explicitly passed, the client automatically defaults the organization name
  to `"local"`.
* **Identity Bypass**: The `dbosctl whoami` command inspects `s.Auth`. If
  `s.Auth != config.AuthBearer`, the client avoids calling the `/v2/users/me`
  endpoint entirely and prints a static local profile:
  * Name: `local`
  * Org: `local`
* **Static Permission Catalogue**: `dbosctl permission list` targets
  `GET /v2/orgs/{orgName}/permissions`. Conductor registers this endpoint in all modes
  (with `orgName` defaulting to `"local"`), returning a static allow-list of grantable
  permissions even when OAuth is inactive.

### Server-side route presence: HTTP 404 vs HTTP 403

The Conductor OpenAPI 3.1 specification contains 16 operations tagged with the
vendor extension `x-dbos-requires-oauth: true`:
* `createRole` (`POST /v2/orgs/{orgName}/roles`)
* `deleteRole` (`DELETE /v2/orgs/{orgName}/roles/{roleName}`)
* `generateSecret` (`POST /v2/orgs/{orgName}/secrets`)
* `getCurrentUser` (`GET /v2/users/me`)
* `getOrg` (`GET /v2/orgs/{orgName}`)
* `grantRole` (`PUT /v2/orgs/{orgName}/members/{username}/roles/{roleName}`)
* `joinOrg` (`POST /v2/orgs/{orgName}/join`)
* `listAuditLogs` (`GET /v2/orgs/{orgName}/audit-logs`)
* `listDomainClaims` (`GET /v2/orgs/{orgName}/domain-claims`)
* `listMembers` (`GET /v2/orgs/{orgName}/members`)
* `listRoles` (`GET /v2/orgs/{orgName}/roles`)
* `registerUser` (`POST /v2/users`)
* `releaseDomainClaim` (`DELETE /v2/orgs/{orgName}/domain-claims/{domain}`)
* `removeMember` (`DELETE /v2/orgs/{orgName}/members/{username}`)
* `requestDomainClaim` (`POST /v2/orgs/{orgName}/domain-claims`)
* `updateOrg` (`PATCH /v2/orgs/{orgName}`)

The specification documents each of these with:
> "Requires OAuth. This operation is not registered when the server runs with
> OAuth disabled (self-hosted no-auth mode), where it responds 404."

### Exit code and client interpretation

In `internal/cli/errors.go`:
* **HTTP 404 (Not Found)** maps to process **exit code 4**. It signals that the
  resource or the route itself does not exist.
* **HTTP 403 (Forbidden)** maps to process **exit code 1**. If the header
  `X-DBOS-Error: past_limit` is returned, the client prints an organization
  plan limit upgrade hint. Otherwise, it prints the problem detail message.
* **HTTP 401 (Unauthorized)** maps to process **exit code 3**, appending
  `run dbosctl login`.

If Relay returned HTTP 403 for an OAuth-gated endpoint in no-auth mode,
`dbosctl` would interpret the condition as an authorization rejection or plan
limitation (exit 1). Instead, Relay must leave OAuth-gated routes **unregistered**
on the HTTP router when running in no-auth mode, causing requests to return
HTTP 404 (exit 4).

## 4. Device authorization flow (authentication reference)

The `dbosctl login` command implements the OAuth 2.0 Device Authorization Grant
([RFC 8628](https://datatracker.ietf.org/doc/html/rfc8628)) over standard OIDC
discovery.

### Configuration requirements

A profile configured for login requires an OIDC block:
* `issuer`: Base URL of the OIDC provider (e.g., `https://login.dbos.dev/` or Keycloak realm).
* `clientID`: Registered public client ID.
* `audience`: Optional resource indicator (required by Auth0: `dbos-cloud-api`).

For DBOS-managed profiles (`cloud.dbos.dev`), the client hardcodes:
* Issuer: `https://login.dbos.dev/`
* Client ID: `6p7Sjxf13cyLMkdwn14MxlH7JdhILled`
* Audience: `dbos-cloud-api`

### Execution sequence

```
dbosctl                      OIDC Provider                  Conductor / Relay
   |                               |                                |
   | 1. Discovery                  |                                |
   |------------------------------>|                                |
   |    GET /.well-known/          |                                |
   |        openid-configuration   |                                |
   |<------------------------------|                                |
   |    returns endpoints          |                                |
   |                               |                                |
   | 2. Request Device Code        |                                |
   |------------------------------>|                                |
   |    POST /device/code          |                                |
   |    (client_id, scope, aud)    |                                |
   |<------------------------------|                                |
   |    device_code, user_code,    |                                |
   |    verification_uri, interval |                                |
   |                               |                                |
   | 3. User browser prompt        |                                |
   |    "Open {uri} and confirm    |                                |
   |     code: {user_code}"        |                                |
   |                               |                                |
   | 4. Polling loop (interval)    |                                |
   |------------------------------>|                                |
   |    POST /oauth/token          |                                |
   |    grant_type=device_code     |                                |
   |<------------------------------|                                |
   |    status / tokens            |                                |
   |    (authorization_pending,    |                                |
   |     slow_down, or 200 OK)     |                                |
   |                               |                                |
   | 5. Best-effort identity lookup|                                |
   |--------------------------------------------------------------->|
   |    GET /v2/users/me (Bearer access_token)                      |
   |<---------------------------------------------------------------|
   |    200 OK: {name, org_name}                                    |
   |                               |                                |
   | 6. Persist credentials        |                                |
   |    saves tokens + org to      |                                |
   |    credentials.yaml           |                                |
```

### Error and status handling during polling

The polling loop inspects the token endpoint response:
* `HTTP 200 OK`: Successful grant. Returns `access_token`, `refresh_token`,
  `token_type`, and `expires_in`.
* `authorization_pending`: User has not yet completed verification. The client
  sleeps for `interval` seconds (default 5s) and retries.
* `slow_down`: Server requested rate reduction. The client increases `interval`
  by 5 seconds (`interval += 5`) and continues polling.
* `expired_token`: The `device_code` expired before completion (deadline
  default 600s). The client terminates with an error.
* `access_denied`: The user declined the authorization request. The client
  terminates with an error.

### Post-login identity resolution

Immediately after acquiring the access token, `dbosctl login` makes a best-effort
request to:
`GET /v2/users/me`
* Timeout: 10 seconds.
* On success (200 OK): Caches `OrgName` and `UserName` into `credentials.yaml`.
  This allows subsequent org-scoped commands to run without requiring `--org`.
* On failure (e.g., user not yet registered in Conductor): The login command
  does not fail. Credentials are saved with empty org and username.

### Credential storage and refresh

Credentials stored in `credentials.yaml` contain:
* `token`: The current access token.
* `refreshToken`: Stored refresh token.
* `expiresAt`: Absolute epoch timestamp (seconds) when token expires.
* `organization`: Cached organization name.
* `userName`: Cached user name.

On subsequent command invocations, if `expiresAt` has passed and the token is
not an API key (`dbos_` prefix), the client executes a token refresh:
* Calls `POST {token_endpoint}` with:
  `grant_type=refresh_token&refresh_token={refreshToken}&client_id={clientID}`
* Updates `credentials.yaml` with the refreshed access token and any rotated
  refresh token.

## 5. Error handling and exit codes

Conductor operations use RFC 9457 problem details (`application/problem+json`)
for error responses:
```json
{
  "status": 404,
  "title": "Not Found",
  "detail": "Workflow 'wf-123' does not exist"
}
```

The client formats error messages by combining `title: detail` if present,
falling back to raw body text, or HTTP status text.

### Process exit code map

| Exit Code | Semantic | Trigger Condition |
| :--- | :--- | :--- |
| `0` | Success | Command completed successfully |
| `1` | General Error | API error (HTTP 400, 403, 500), runtime failure |
| `2` | Usage Error | Invalid command syntax, unknown flag, bad flag type |
| `3` | Auth Required | HTTP 401 Unauthorized; appends hint to run `dbosctl login` |
| `4` | Not Found | HTTP 404 Not Found (resource or endpoint missing) |
| `130` | Interrupted | Process received SIGINT or SIGTERM |

## 6. Command surface inventory

The table below catalogs every command implemented in `dbosctl`, including its
HTTP mapping, URL template, query parameters, payload shape, and resolution flags.

### Global flags
* `--version`: Prints version string.
* `--help`: Displays command usage.

### Commands

#### Identity & Configuration

| Command | HTTP Method | Endpoint Path | Query / Body Parameters | Resolution Flags |
| :--- | :--- | :--- | :--- | :--- |
| `dbosctl whoami` | `GET` | `/v2/users/me` | None. In no-auth mode, bypassed client-side. | `--profile`, `--url`, `-o` / `--output` |
| `dbosctl login` | OIDC | RFC 8628 device flow | Discovers endpoints, polls token endpoint, then calls `GET /v2/users/me`. | `--profile`, `--url` |
| `dbosctl logout` | Local | None | Drops stored credentials from `credentials.yaml`. | `--profile` |
| `dbosctl config list` | Local | None | Lists profiles in `config.yaml`. | None |
| `dbosctl config show [profile]` | Local | None | Displays configuration fields for profile. | None |
| `dbosctl config use <profile>` | Local | None | Sets `current` profile pointer in `config.yaml`. | None |
| `dbosctl config set <profile>` | Local | None | Updates profile settings in `config.yaml`. | `--url`, `--org`, `--app`, `--auth`, `--issuer`, `--audience`, `--client-id`, `--managed`, `--domain` |

#### API Keys (`api-key`, aliases: `token`, `apikey`)

| Command | HTTP Method | Endpoint Path | Query / Body Parameters | Resolution Flags |
| :--- | :--- | :--- | :--- | :--- |
| `dbosctl api-key list` | `GET` | `/v2/orgs/{orgName}/tokens` | None. | `--profile`, `--url`, `--org`, `-o` / `--output` |
| `dbosctl api-key create <name>` | `POST` | `/v2/orgs/{orgName}/tokens/{tokenName}` | Body: `{"appNames": [...], "permissions": [...]}`. Flag: repeatable `--app`, repeatable `--permission`. | `--profile`, `--url`, `--org`, `-o` / `--output` |
| `dbosctl api-key delete <name>` | `DELETE` | `/v2/orgs/{orgName}/tokens/{tokenName}` | None. | `--profile`, `--url`, `--org` |

#### Applications (`app`)

| Command | HTTP Method | Endpoint Path | Query / Body Parameters | Resolution Flags |
| :--- | :--- | :--- | :--- | :--- |
| `dbosctl app list` | `GET` | `/v2/orgs/{orgName}/apps` | None. | `--profile`, `--url`, `--org`, `-o` / `--output` |
| `dbosctl app register <name>` | `PUT` | `/v2/orgs/{orgName}/apps/{appName}` | Body: `{"privateMode": bool}`. Flag: `--private-mode`. | `--profile`, `--url`, `--org` |
| `dbosctl app delete <name>` | `DELETE` | `/v2/orgs/{orgName}/apps/{appName}` | Interactive prompt or `--force`. | `--profile`, `--url`, `--org`, `--force` |
| `dbosctl app get <name>` | `GET` | `/v2/orgs/{orgName}/apps/{appName}` | None. | `--profile`, `--url`, `--org`, `-o` / `--output` |
| `dbosctl app versions <name>` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/versions` | None. | `--profile`, `--url`, `--org`, `-o` / `--output` |
| `dbosctl app executors <name>` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/executors` | None. | `--profile`, `--url`, `--org`, `-o` / `--output` |
| `dbosctl app metrics <name>` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/metrics` | Query: `startTime` (timestamp), `endTime` (timestamp). Flag: `--since` (duration, default: 24h). | `--profile`, `--url`, `--org`, `-o` / `--output` |
| `dbosctl app update <name>` | `PATCH` | `/v2/orgs/{orgName}/apps/{appName}` | Body (sparse patch): `{"executorTimeoutSecs": int64, "gcRowsThreshold": int64, "gcTimeThresholdMs": int64, "globalTimeoutMs": int64, "privateMode": bool}`. Flags: `--executor-timeout-secs`, `--gc-rows-threshold`, `--gc-time-threshold-ms`, `--global-timeout-ms`, `--private-mode`. | `--profile`, `--url`, `--org` |
| `dbosctl app set-version <name> <version>` | `PATCH` | `/v2/orgs/{orgName}/apps/{appName}/versions/latest` | Body: `{"versionName": "<version>"}`. | `--profile`, `--url`, `--org` |

#### Queues (`queue`)

| Command | HTTP Method | Endpoint Path | Query / Body Parameters | Resolution Flags |
| :--- | :--- | :--- | :--- | :--- |
| `dbosctl queue list` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/queues` | None. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |
| `dbosctl queue get <name>` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/queues/{queueName}` | None. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |

#### Schedules (`schedule`)

| Command | HTTP Method | Endpoint Path | Query / Body Parameters | Resolution Flags |
| :--- | :--- | :--- | :--- | :--- |
| `dbosctl schedule list` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/schedules` | None. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |
| `dbosctl schedule get <name>` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}` | None. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |
| `dbosctl schedule pause <name>` | `POST` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/pause` | None. | `--profile`, `--url`, `--org`, `-a` / `--app` |
| `dbosctl schedule resume <name>` | `POST` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/resume` | None. | `--profile`, `--url`, `--org`, `-a` / `--app` |
| `dbosctl schedule trigger <name>` | `POST` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/trigger` | None. Returns 201 with `{"workflow_id": "..."}`. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |
| `dbosctl schedule backfill <name>` | `POST` | `/v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/backfill` | Body: `{"startTime": timestamp, "endTime": timestamp}`. Flags: `--since`, `--until` (required). | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |

#### Workflows (`workflow`, alias: `wf`)

| Command | HTTP Method | Endpoint Path | Query / Body Parameters | Resolution Flags |
| :--- | :--- | :--- | :--- | :--- |
| `dbosctl workflow list` | `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/search` | Body: `{"workflowIds": [...], "user": [...], "status": [...], "workflowName": [...], "appVersion": [...], "queueName": [...], "limit": int64, "offset": int64, "sortDesc": bool, "queuesOnly": bool, "startTime": timestamp, "endTime": timestamp}`. Flags: `-l` / `--limit`, `--offset`, `--id`, `-u` / `--user`, `-s` / `--status`, `-n` / `--name`, `--app-version`, `--queue`, `--since`, `--until`, `--desc`, `--queued`. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` (supports `-o ids`) |
| `dbosctl workflow get <id>` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}` | None. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |
| `dbosctl workflow steps <id>` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/steps` | None. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |
| `dbosctl workflow events <id>` | `GET` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/events` | None. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |
| `dbosctl workflow cancel <id>...` | `POST` | Single ID: `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/cancel`<br>Multi ID: `/v2/orgs/{orgName}/apps/{appName}/workflows/bulk-cancel` | Body: `{"cancelChildren": bool}` (single) or `{"workflowIds": [...], "cancelChildren": bool}` (multi). Positional args accept `-` to read IDs from stdin. Flag: `--children`. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` (`-o ids`) |
| `dbosctl workflow resume <id>...` | `POST` | Single ID: `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/resume`<br>Multi ID: `/v2/orgs/{orgName}/apps/{appName}/workflows/bulk-resume` | Body: `{"queueName": "<queue>"}` (single) or `{"workflowIds": [...], "queueName": "<queue>"}` (multi). Positional args accept `-` to read IDs from stdin. Flag: `--queue`. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |
| `dbosctl workflow delete <id>...` | Single: `DELETE`<br>Multi: `POST` | Single ID: `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}`<br>Multi ID: `/v2/orgs/{orgName}/apps/{appName}/workflows/bulk-delete` | Single query: `delete_children=bool`. Multi body: `{"workflowIds": [...], "deleteChildren": bool}`. Positional args accept `-` to read IDs from stdin. Flag: `--children`. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |
| `dbosctl workflow fork <id>` | `POST` | `/v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/fork` | Body: `{"newWorkflowId": "...", "startStep": int32, "queueName": "...", "appVersion": "..."}`. Returns 201 with `{"workflow_id": "..."}`. Flags: `--new-id`, `--start-step`, `--queue`, `--app-version`. | `--profile`, `--url`, `--org`, `-a` / `--app`, `-o` / `--output` |

#### Permissions (`permission`)

| Command | HTTP Method | Endpoint Path | Query / Body Parameters | Resolution Flags |
| :--- | :--- | :--- | :--- | :--- |
| `dbosctl permission list` | `GET` | `/v2/orgs/{orgName}/permissions` | None. Registered in all auth modes. | `--profile`, `--url`, `--org`, `-o` / `--output` |

#### System Database Commands (`sysdb`)

The `sysdb` command group (`sysdb migrate`, `sysdb reset`, `sysdb rename`) connects
directly to PostgreSQL or CockroachDB using `--db-url` or `$DBOS_SYSTEM_DATABASE_URL`.
These commands never call the Conductor or Relay HTTP API and require no profile.
Under Relay's architectural invariants, Relay never executes direct raw SQL against
an application's system database; sysdb operations remain exclusively client-side.

#### Version Command (`version`)

`dbosctl version` inspects local Go build info and VCS stamps baked into the
binary; it makes no network requests.

## 7. Conformance and acceptance checklist

This checklist defines the criteria for acceptance testing the REST API and
authentication layer. Conformance test suites in `tests/conformance/` validate HTTP requests and payloads
against Relay running as the target server, asserting behavior matching the `dbosctl` client protocol.

### REST API Conformance Checklist

- [ ] **No-Auth Defaults**:
  - Request with no token defaults organization to `"local"`.
  - Routes tagged `x-dbos-requires-oauth` are unregistered and return HTTP 404
    (`dbosctl` exits with code 4).
- [ ] **Application Operations**:
  - `app list`: `GET /v2/orgs/{orgName}/apps` returns list of application objects.
  - `app register`: `PUT /v2/orgs/{orgName}/apps/{appName}` creates application record.
  - `app get`: `GET /v2/orgs/{orgName}/apps/{appName}` returns application metadata.
  - `app update`: `PATCH /v2/orgs/{orgName}/apps/{appName}` applies sparse update for timeout,
    retention, and private mode.
  - `app set-version`: `PATCH /v2/orgs/{orgName}/apps/{appName}/versions/latest` records active version.
  - `app versions`: `GET /v2/orgs/{orgName}/apps/{appName}/versions` returns registered versions.
  - `app executors`: `GET /v2/orgs/{orgName}/apps/{appName}/executors` returns connected
    executors reported over WebSocket.
  - `app metrics`: `GET /v2/orgs/{orgName}/apps/{appName}/metrics` returns point-in-time metrics
    for given time window.
  - `app delete`: `DELETE /v2/orgs/{orgName}/apps/{appName}` removes application.
- [ ] **Queue Operations**:
  - `queue list`: `GET /v2/orgs/{orgName}/apps/{appName}/queues` returns queue configurations.
  - `queue get`: `GET /v2/orgs/{orgName}/apps/{appName}/queues/{queueName}` returns queue details.
- [ ] **Schedule Operations**:
  - `schedule list`: `GET /v2/orgs/{orgName}/apps/{appName}/schedules` returns schedule definitions.
  - `schedule get`: `GET /v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}` returns schedule details.
  - `schedule pause`: `POST /v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/pause` pauses schedule.
  - `schedule resume`: `POST /v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/resume` resumes schedule.
  - `schedule trigger`: `POST /v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/trigger` starts workflow and returns HTTP 201 with `workflow_id`.
  - `schedule backfill`: `POST /v2/orgs/{orgName}/apps/{appName}/schedules/{scheduleName}/backfill` starts workflow backfill window.
- [ ] **Workflow Search and Inspection**:
  - `workflow list`: `POST /v2/orgs/{orgName}/apps/{appName}/workflows/search` correctly
    processes all filter combinations (IDs, status, names, queue, pagination,
    time ranges, order, queued-only).
  - `workflow get`: `GET /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}` returns complete
    workflow record.
  - `workflow steps`: `GET /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/steps` returns step
    execution list.
  - `workflow events`: `GET /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/events` returns event
    key-value list.
- [ ] **Workflow Mutations**:
  - `workflow cancel` (single): `POST /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/cancel`
    dispatches cancel command to executor over WebSocket.
  - `workflow cancel` (bulk): `POST /v2/orgs/{orgName}/apps/{appName}/workflows/bulk-cancel`
    dispatches bulk cancellation.
  - `workflow resume` (single): `POST /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/resume`.
  - `workflow resume` (bulk): `POST /v2/orgs/{orgName}/apps/{appName}/workflows/bulk-resume`.
  - `workflow delete` (single): `DELETE /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}`.
  - `workflow delete` (bulk): `POST /v2/orgs/{orgName}/apps/{appName}/workflows/bulk-delete`.
  - `workflow fork`: `POST /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/fork` creates
    forked workflow and returns HTTP 201 with new `workflow_id`.
- [ ] **Permissions**:
  - `permission list`: `GET /v2/orgs/{orgName}/permissions` returns list of grantable
    permissions in all modes.
- [ ] **API Keys**:
  - `api-key list`: `GET /v2/orgs/{orgName}/tokens` returns API keys.
  - `api-key create`: `POST /v2/orgs/{orgName}/tokens/{tokenName}` mints API key with secret
    returned once in HTTP 201 response.
  - `api-key delete`: `DELETE /v2/orgs/{orgName}/tokens/{tokenName}` revokes API key.
- [ ] **Problem Details Format**:
  - Non-2xx responses emit `application/problem+json` matching `api.ErrorModel`
    with `status`, `title`, and `detail`.

### Authentication Conformance Checklist (OIDC)

- [ ] **Token Authentication**:
  - Accepts `Authorization: Bearer <token>` carrying either a minting `dbos_`
    API key or an OIDC JWT.
  - Validates API key scoping (application and permission constraints).
  - Returns HTTP 401 when token is missing or invalid on authenticated routes,
    prompting `dbosctl` to exit with code 3.
- [ ] **Device Flow Identity Resolution**:
  - `GET /v2/users/me` registered and active when OIDC is configured.
  - Returns current user profile with `name`, `email`, `org_name`, `subscriptionPlan`,
    and `isDbosAdmin`.
  - Allows `dbosctl login` to cache user and organization identity upon login.
- [ ] **Organization Membership & Management**:
  - Registers all 16 `x-dbos-requires-oauth` endpoints when OIDC is active.
  - Supports role creation, listing, granting, and deletion.
  - Supports member listing, joining, and removal.
  - Supports domain claims request, listing, and release.
  - Supports organization updating and secret generation.
- [ ] **Plan Limits Header**:
  - Emits header `X-DBOS-Error: past_limit` on HTTP 403 when an organization
    exceeds its plan limit, verifying the client prints the plan upgrade hint.
