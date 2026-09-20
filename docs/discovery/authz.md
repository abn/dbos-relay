---
type: Reference
title: Permission model and API key format
description: Specification of permissions, roles, API key format, scoping, and no-auth mode behaviour.
status: draft
---

# Permission model and API key format

This document specifies the authorization model, permission catalog, role
hierarchy, API key structure, and no-auth mode semantics for Relay. The
specifications are derived under clean-room rules from the permitted DBOS
Transact SDKs, the `dbosctl` command-line client, and the Conductor OpenAPI
specification.

## Provenance and clean-room citations

All specifications in this document are derived from permitted sources:

* **dbosctl client source and OpenAPI specification**:
  Repository `https://github.com/dbos-inc/dbos-ctl`, commit `9d14ed3f0ccddb84cd3390e0bddbcfb9ea9a32a6`
  * API key management: `internal/cli/apikey.go` (lines 13-19, 48-50, 78-118, 141-156)
  * Permission inspection: `internal/cli/permission.go` (lines 9-20, 27-48)
  * Identity resolution: `internal/cli/whoami.go` (lines 37-41, 61-94)
  * Client transport: `internal/client/client.go` (lines 28-32, 54-60)
  * Configuration resolution: `internal/config/resolve.go` (lines 103-109)
  * OAuth-gated operation registry: `internal/api/oauth_gated.go` (lines 9-26)
  * OpenAPI schemas and routes: `api/spec/openapi.json`
    * Schemas: `Token` (`#/components/schemas/Token`), `TokenCreated` (`#/components/schemas/TokenCreated`),
      `CreateTokenInputBody` (`#/components/schemas/CreateTokenInputBody`), `RoleOutput` (`#/components/schemas/RoleOutput`),
      `CreateRoleInputBody` (`#/components/schemas/CreateRoleInputBody`), `CreateRoleOutputBody` (`#/components/schemas/CreateRoleOutputBody`)
    * Paths: `#/paths/~1v2~1orgs~1{orgName}~1permissions`,
      `#/paths/~1v2~1orgs~1{orgName}~1tokens`,
      `#/paths/~1v2~1orgs~1{orgName}~1roles`
  * Conformance tests: `internal/cli/apikey_test.go` (lines 16-35, 78-134),
    `internal/cli/permission_test.go` (lines 45-61, 79-97),
    `internal/cli/integration_test.go` (lines 133-174, 403-429)

* **dbos-transact-go**:
  Repository `https://github.com/dbos-inc/dbos-transact-go`, commit `ab56911fdd78552e1e7fe648cff7c831a1e760c8`
  * WebSocket connection URL and key transmission: `dbos/conductor.go` (lines 40, 78-95)
  * Configuration defaults: `dbos/dbos.go` (lines 674, 690)
  * Role tracking in workflow contexts: `dbos/workflow.go` (lines 36, 1228, 1491, 1513, 1833, 2040)
  * System database persistence: `dbos/internal/sysdb/system_database.go` (lines 1421, 1716-1772)

* **dbos-transact-py**:
  Repository `https://github.com/dbos-inc/dbos-transact-py`, commit `833794f7a1138bacf75ff6d88647a33eb5e35e52`
  * WebSocket connection URL: `dbos/_conductor/conductor.py` (lines 51-53)
  * Configuration options: `dbos/_dbos.py` (lines 443-477, 744-776), `dbos/_dbos_config.py` (lines 46-48, 84-86)

* **dbos-transact-ts**:
  Repository `https://github.com/dbos-inc/dbos-transact-ts`, commit `d8c4974cca6cc84b296f3b8edfbbb41627ddd47e`
  * Protocol serialization: `src/conductor/protocol.ts` (line 276)
  * Context and execution roles: `src/context.ts` (line 26), `src/system_database.ts` (lines 311, 653)

* **dbos-transact-java**:
  Repository `https://github.com/dbos-inc/dbos-transact-java`, commit `1248174f393bd97f9973ec83cbc6e42b6e319ed1`
  * Default role strings: `transact/src/test/java/dev/dbos/transact/database/SystemDatabaseTest.java` (lines 721-722, 831-832)
  * Queue role validation: `transact/src/test/java/dev/dbos/transact/queue/DynamicQueuesTest.java` (lines 756-757)

## 1. Specification summary

Summary of permissions, default roles, application scoping, and key format
confirmed from public documentation and client source:

1. **Permission list**: `application.read`, `application.write`,
   `websocket.connect`, and `metric.read` form the grantable permission
   catalog exposed by `GET /v2/orgs/{orgName}/permissions`.
2. **Default roles**: `admin`, `operator`, and `viewer` (read-only), with
   `admin` also represented as a user profile attribute (`isDbosAdmin`).
3. **Key scoping**: Keys are scoped by organization, optionally restricted to
   one or more application names via `appNames` (`appIds` in responses), and
   optionally constrained to a specific permission subset.
4. **Key prefix**: Plaintext API keys begin with the prefix `dbos_`.

## 2. Permission catalog

Permissions are atomic strings granting capability over resources within an
organization.

### Core permissions

| Permission | Scope | Operations permitted |
| --- | --- | --- |
| `application.read` | Application | Read-only inspection of applications, workflows, execution steps, workflow events, notifications, streams, queues, schedules, metrics, alerting rules, and autoscaling policies. Backs `dbosctl app list`, `dbosctl workflow list`, `dbosctl queue list`, and dashboard monitoring. |
| `application.write` | Application | Mutations on application resources: registering apps, deleting apps, updating settings, cancelling workflows, resuming workflows, forking workflows, importing workflows, bulk operations (bulk-cancel, bulk-delete, bulk-resume, bulk-fork), schedule management (pause, resume, trigger, backfill), alerting rule creation and deletion, autoscaling policy modification. |
| `websocket.connect` | Application | Establishing executor WebSocket connections to the control plane at `/websocket/{appName}/{apiKey}`. Required by runtime worker processes. |
| `metric.read` | Application | Reading application metrics, including the Prometheus-compatible `/v1/metrics` scrape endpoint. Accepted alongside `application.read` on that endpoint. |

### Catalog endpoint

The permission catalog is fetched via:
* `GET /v2/orgs/{orgName}/permissions` (`listPermissions`)
* Tag: `Roles`
* Auth requirement: Not OAuth-gated (`x-dbos-requires-oauth: false`). Served in
  both authenticated and no-auth modes.
* Response: JSON array of strings (`[]string`), containing the supported
  grantable permissions: `["application.read", "application.write", "websocket.connect", "metric.read"]`.

## 3. Role hierarchy and structure

Roles aggregate permissions into assignable identities.

### Schema definitions

Defined in `openapi-3.1.json`:
* `RoleOutput` (`#/components/schemas/RoleOutput`):
  * `name` (string, required): Role identifier. Length 3 to 30 characters,
    matching pattern `^[a-zA-Z0-9_]+$`.
  * `permissions` (array of string, required): Permissions granted to the role.
  * `isGlobal` (boolean, required): Distinguishes built-in global roles from
    custom organization-level roles.
* `CreateRoleInputBody` (`#/components/schemas/CreateRoleInputBody`):
  * `name` (string, required): Length 3 to 30 characters, pattern `^[a-zA-Z0-9_]+$`.
  * `permissions` (array of string, nullable): Initial permission set.

### Standard roles

| Role name | `isGlobal` | Permissions granted | Purpose |
| --- | --- | --- | --- |
| `admin` | `true` | `application.read`, `application.write`, `websocket.connect`, `metric.read`, plus organization administrative actions | Full administrative access. Can manage members, roles, domain claims, and API keys. Users with global administrative rights carry `isDbosAdmin: true` in `UserProfile`. |
| `operator` | `true` | `application.read`, `application.write`, `websocket.connect`, `metric.read` | Operational lifecycle management. Can run migrations, register apps, cancel/resume/fork workflows, manage schedules and queues, without organization membership management. |
| `viewer` | `true` | `application.read`, `metric.read` | Read-only inspection across applications, workflows, queues, schedules, and metrics. Cannot alter operational state or connect executors. |

### Role assignment and execution context

* Organization membership roles are assigned via `PUT /v2/orgs/{orgName}/members/{username}/roles/{roleName}` (`grantRole`).
* During workflow execution, active user roles are captured in the execution
  context (`authenticatedRoles: []string`) and persisted in the application
  system database (`workflow_status.authenticated_roles` column) as JSON arrays
  (for example, `["admin", "operator"]`).

## 4. API key format and cryptographic properties

API keys (referred to as tokens in Conductor REST paths) authenticate automated
clients and SDK executors.

### Format specification

| Parameter | Specification | Rationale |
| --- | --- | --- |
| Key prefix | `dbos_` | 5 ASCII characters. Matches upstream convention verified across SDKs, CLI, and integration tests (`apikey_test.go:80`, `integration_test.go:148`). Allows secret scanners to detect exposed keys. |
| Entropy | 32 bytes (256 bits) | Generated via a cryptographically secure pseudorandom number generator (`crypto/rand`). Provides 256 bits of security against brute-force search. |
| Alphabet | Base64 URL (unpadded) | RFC 4648 URL-safe alphabet (`[A-Za-z0-9_-]`). Eliminates URL-encoding issues when transmitted in HTTP headers or WebSocket URL path segments. |
| Encoded length | 43 characters | Base64 encoding of 32 bytes unpadded: `ceil(32 * 8 / 6) = 43` characters. |
| Total plaintext length | 48 characters | 5 characters (`dbos_`) + 43 characters base64url = 48 characters total. |
| Lookup prefix | 12 characters | First 12 characters of the plaintext key (`dbos_` + 7 characters base64url). Stored cleartext in database index. |
| Storage hash | SHA-256 (32 bytes) | SHA-256 digest of full plaintext key. Stored as binary `BYTEA` (or 64-character lowercase hex string). |

### Storage and lookup mechanics

1. **Cleartext lookup prefix**:
   * Stored in the `api_keys.lookup` column with a `UNIQUE` index.
   * Enables `O(1)` database row lookup on incoming authentication requests.
   * Does not contain sufficient entropy to authenticate the caller.
2. **Secret hash storage**:
   * Plaintext key is hashed with SHA-256: `hash = sha256(plaintext)`.
   * Stored in `api_keys.key_hash`.
   * Verification uses constant-time comparison (`crypto/subtle.ConstantTimeCompare`)
     between the candidate key's SHA-256 digest and the stored digest.
3. **Single presentation**:
   * The plaintext key is returned exactly once in the response to
     `POST /v2/orgs/{orgName}/tokens/{tokenName}` (`TokenCreated.token`).
   * The CLI outputs the raw key on stdout and prints a warning on stderr:
     `API key "..." created: store this secret now, it is not shown again`
     (`apikey.go:115`).
   * The plaintext is never persisted in Relay's database and never logged.
4. **Password hash vs cryptographic hash**:
   * High-entropy keys (256 bits of CSPRNG entropy) have no dictionary or
     low-entropy attack vectors.
   * Slow key-derivation functions (such as bcrypt, scrypt, or argon2) are
     unnecessary and would severely bottleneck WebSocket connection handshakes
     and high-throughput API endpoints. SHA-256 provides immediate validation
     with constant-time comparison.

## 5. Scope rules and application isolation

API keys are scoped within an organization along two dimensions: application
names and permissions.

### Application scoping (`appNames` / `appIds`)

In `CreateTokenInputBody`:
```json
{
  "appNames": ["order-service", "billing-service"],
  "permissions": ["application.read", "websocket.connect"]
}
```

* **Unscoped keys (`(all)`)**:
  * When `appNames` is omitted or empty (`nil`), the key is unscoped.
  * An unscoped key is valid for all applications within the owning organization.
  * In CLI output: displayed as `(all)` (`apikey.go:150-156`).
* **Scoped keys**:
  * When `appNames` contains one or more application identifiers, the key is
    strictly restricted to the specified applications.
  * Requests targeting an unlisted application receive `403 Forbidden`.
  * WebSocket connection attempts at `/websocket/{appName}/{apiKey}` where
    `appName` does not match the key's allowed applications are rejected during
    the upgrade handshake.

### Permission scoping

* When `permissions` is specified in key creation, the key possesses only the
  explicitly granted subset.
* When `permissions` is omitted or empty, the key inherits the default
  capabilities of the creator's role within the organization.

## 6. No-auth mode

Relay supports running with authentication disabled, corresponding to
self-hosted local deployments where no OIDC identity provider is configured.

### Invariants in no-auth mode

1. **Default organization**:
   * The organization defaults to `"local"` (`internal/config/resolve.go:107-109`).
   * All registered applications and workflows belong to `"local"`.
2. **Implicit full permissions**:
   * Every incoming HTTP request to registered operational endpoints carries
     implicit full permissions (`application.read`, `application.write`,
     `websocket.connect`) for the `"local"` organization.
   * No `Authorization: Bearer <token>` header is required.
3. **Executor WebSocket connection**:
   * WebSocket URL path: `/websocket/{appName}/{apiKey}`.
   * In no-auth mode, the `{apiKey}` path parameter is accepted unconditionally.
     Executors can supply any string (for example, `"local"`, `"none"`, or
     `"test-key"`).
4. **Absence of OAuth-gated endpoints**:
   * Operations marked with `x-dbos-requires-oauth: true` in the OpenAPI
     specification respond with 404 Problem Details from their handlers in no-auth
     mode, and any HTTP method targeting these routes is rejected with 404.
   * Probing or invoking an OAuth-gated endpoint returns `404 Not Found`.
   * The server never returns `401 Unauthorized` or `403 Forbidden` for
     unregistered identity endpoints.
   * Gated operations (16 total, per upstream dbos-ctl `internal/api/oauth_gated.go`):
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
5. **Permissions route available**:
   * `GET /v2/orgs/{orgName}/permissions` is not OAuth-gated.
   * In no-auth mode, it responds with status `200 OK` and the standard
     permission catalog (`["application.read", "application.write", "websocket.connect", "metric.read"]`).
6. **Local identity resolution**:
   * `dbosctl whoami` inspects the profile's auth setting (`AuthNone`).
   * Because `/v2/users/me` is not registered, `dbosctl whoami` directly renders
     the static identity without making a network request:
     `{"name": "local", "orgName": "local"}` (`whoami.go:37-41, 61-94`).

## Audit log

Relay records an audit entry for every mutating operation in the Conductor
taxonomy (`https://docs.dbos.dev/production/audit-logs`): application
registration, update, deletion, and latest-version changes; workflow cancel,
resume, fork, fork-from-failure, delete, import, and bulk operations;
schedule pause, resume, trigger, and backfill; alerting rule creation and
deletion; API key creation and revocation; role creation, deletion, and
grants; organization updates; and user joins and removals. Reads are never
audited. Each entry records success or failure, so denied and invalid
requests appear alongside completed mutations, except where the
organization itself cannot be resolved (there is no scope to record
under) and except join-secret generation, which has no upstream operation
and is not recorded.

Entries store a details envelope with the subject (`subject_type` of `user`
or `api_key`, stable `subject_id`, and human-readable `subject_display`
preserved after deletion), the target (`target_type` and `target_id`,
omitted for bulk operations), the caller IP, and operation-specific
context (`application_name`, `workflow_ids`, `permissions`,
`applications`, `role_name`, `new_name`, `audit_log_retention_days`,
`private_mode`). Listing (`GET /v2/orgs/{orgName}/audit-logs`) accepts
`startTime`, `endTime`, `operation` (exact match), `subject` (matched
against display name, stable id, and legacy username column), and `target`
(exact target id) filters with
`limit` (default 100, maximum 1000, larger values rejected) and `offset`
paging, newest first.

Retention defaults to 90 days and is configurable per organization between
7 and 3650 days via `PATCH /v2/orgs/{orgName}` (`audit_log_retention_days`;
`new_name` renames the organization). A PATCH changing nothing returns 204
without recording an entry. Expired entries are purged hourly.
Domain-claim mutations are recorded under `domain_claim.create` and
`domain_claim.delete`, a Relay extension outside the upstream taxonomy.
