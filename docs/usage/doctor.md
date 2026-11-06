---
type: HowTo
title: Doctor preflight
description: Run read-only migration and production readiness checks before and after a Relay deployment.
status: draft
---

# Doctor preflight

`relay doctor` is a read-only preflight command. It checks the control-plane
database, its migration and rollback state, the application system databases,
the declarative manifest, and the Direct mode probe configuration. It is safe to
run against a production deployment.

Doctor never applies a migration, never writes to an application system
database, and never prints a credential. Checks that cannot be performed safely
are reported as skipped instead of being simulated.

## Usage

Embedded control plane:

```bash
./bin/relay doctor --embedded
```

PostgreSQL control plane with a declarative manifest:

```bash
export RELAY_DATABASE_URL="postgres://relay:...@db.internal:5432/relay?sslmode=require"
export RELAY_CONFIG=/etc/relay/relay.yaml
./bin/relay doctor
```

Direct mode application host:

```bash
export DBOS_APP_NAME="order-service"
export DBOS__VMID="order-service-0"
export RELAY_PROBE_URL="https://relay.internal:8090"

relay doctor --file /etc/relay/relay.yaml --database-url "$RELAY_DATABASE_URL"
```

Flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--database-url` | `RELAY_DATABASE_URL` | Control-plane database URL. |
| `--embedded` | false | Use `sqlite://./data/relay.db` when no URL is supplied. |
| `-f`, `--file` | `RELAY_CONFIG` | Declarative manifest to check. |
| `--relay-url` | `RELAY_PROBE_URL`, then `RELAY_URL`, then `http://127.0.0.1:8090` | Relay base URL for the health check. |
| `--probe-app` | `DBOS_APP_NAME` | Direct application name for the probe identity check. |
| `--probe-executor-id` | `DBOS__VMID` | Executor ID for the probe identity check. |
| `--timeout` | `10s` | Timeout for each network operation. |

## What it checks

| Check | What it proves |
| --- | --- |
| `control-plane.connectivity` | The control-plane database accepts a connection. |
| `control-plane.read` | The Relay role can read control-plane tables. |
| `control-plane.migrations` | The applied schema version, the dirty flag, and the binary's latest migration. |
| `control-plane.write` | The Relay role can write, proved by a no-op update in a transaction that is rolled back. |
| `rollback.control-plane` | A paired down migration exists for the applied version and the schema is not dirty. |
| `rollback.snapshot` | Reported as skipped because Relay cannot verify that a database snapshot exists. |
| `manifest.load` | The declarative manifest parses and validates. |
| `data-plane.manifest[app]` | The application is declared and provisioned, and its connection resolves. |
| `data-plane.connectivity[app]` | The official MIT Go client can connect to the application system database without migrating it. |
| `direct.schema[app]` | The application system database is inside the Direct mode schema window 123 through 125. |
| `data-plane.read[app]` | A bounded workflow read succeeds through the SDK client. |
| `data-plane.write[app]` | Read-write permission is either not required or reported as skipped because no non-destructive write probe exists. |
| `schedules[app]` | Schedule names are unique and schedule ownership matches the application. |
| `rollback.application[app]` | Reported as skipped because Relay cannot verify an application database snapshot or the prior control plane. |
| `probe.secret` | `RELAY_INTERNAL_SECRET` is present when Direct mode or authentication needs it. |
| `probe.endpoint` | `/healthz` is reachable from this host. No heartbeat is posted. |
| `probe.identity` | The probe application is Direct mode and has an executor identity. |

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | No failing check. Warnings and skipped checks are allowed. |
| `1` | At least one check failed. The report names each failure and a remedy. |
| `2` | Doctor could not start, for example because no database URL was supplied. |

A failing check is a production blocker. A warning is not, but it should be
reviewed before the deployment is declared ready. A skipped check is a
limitation of what Relay can prove without a destructive action.

## Reading the result

Each line has the check identifier, its status, and a short detail. A failure
also carries a `remedy` line. For example:

```text
FAIL  control-plane.migrations    schema is dirty at version 9; Relay refuses to start or migrate until it is resolved.
                                  remedy: Inspect the database, then run 'relay migrate force --version 9 --confirm' after resolving the failed migration.
```

Fix failures in this order:

1. Control-plane connectivity and migration state.
2. Manifest application and data-plane registration preconditions.
3. Application system database connectivity, schema window, and read access.
4. Probe secret, endpoint, and identity.

## Safety boundaries

Doctor follows the same data-plane rules as Relay itself:

- The control-plane database is Relay's own database. Doctor reads it, and its
  only write is a no-op update rolled back inside a transaction.
- An application system database is reached only through the official MIT Go
  client. Doctor never issues raw SQL against `dbos.*` tables.
- The SDK client is created with migrations skipped, so doctor never creates or
  changes an application schema.
- The probe endpoint check is a `GET /healthz`. Doctor does not post a
  heartbeat, register an executor, or start a grace period.
- Connection strings and secret values are redacted from every reported error.

## Checks that are reported, not performed

Some checks cannot be made without a destructive write or a credential Doctor
must not handle. They appear as `SKIP` or `WARN` with an explicit reason:

- **Application read-write permission**: there is no non-destructive write probe
  for an application system database. Doctor verifies reads and reports the
  write check as skipped. Verify a read-write role with one safe mutation in
  staging.
- **Database snapshots**: Relay cannot see the backup system. Take and test a
  snapshot before migrating or rolling back.
- **Direct mode rollback**: Relay does not manage the application database or a
  proprietary connector. Record the prior control plane and take an application
  system database snapshot before a Direct cutover.
- **Exact schema version**: the SDK client verifies the schema but only reports
  the version in a debug log. Doctor captures that log when available. If it is
  not reported, Doctor warns that the exact version could not be confirmed.
- **Probe identity on the Relay host**: the application name and executor ID
  belong on the application host. Doctor warns when they are absent and lets
  you pass them explicitly with `--probe-app` and `--probe-executor-id`.

## Related

- [Quickstart](quickstart.md) for a first deployment.
- [Operations and migrations](operations.md) for migration and rollback
  procedures.
- [Direct mode](direct.md) for application database and probe setup.
- [Supported SDKs](supported-sdks.md) for the Direct mode schema window.
- [Declarative operations](declarative.md) for the manifest format.
