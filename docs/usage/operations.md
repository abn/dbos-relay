---
type: Guide
title: Operations and migrations
description: Operational procedures for Relay schema migrations, rolling upgrades, and rollbacks.
status: stable
---

# Operations and migrations

This document covers operational procedures for database migrations, rolling upgrades,
and schema rollback strategies in Relay deployments.

## Upgrade notes

### Direct mode release

This release adds Direct mode and retires the legacy data-plane fallback that
pinned the Go SDK at `v1.3.0`. Relay now pins the MIT Go client at `v1.7.0`,
which requires application schema 123 and tolerates newer schemas. The supported
window is schema 123 through 125; schema 123 and 125 are tested, and 124 is
covered by the client's migration tolerance rather than a separate test cell.

**Who is affected**

- Deployments that used `data_plane` as an outage fallback for applications at
  schema 107 through 122. That fallback is no longer available. Keep those
  applications in Attached mode, or upgrade their system database to schema 123
  through 125 before configuring Direct mode.
- Applications on Go `v1.7.0` or `v1.8.0`, Python `3.3.0` or `3.4.0`, and
  TypeScript `5.3.0` or `5.4.0` that need a control plane. Configure Direct mode
  and, where automatic recovery is required, a liveness signal.

**Who is not affected**

- Attached deployments on Go `v1.6.0` and below, Python `3.2.0` and below,
  TypeScript `5.2.11` and below, and Java `1.2.0` without a `data_plane` entry.
  Their executor protocol path and application configuration are unchanged.
- Deployments that do not use `data_plane`.

**Upgrade steps**

1. Snapshot the Relay control-plane database and every application system
   database that Relay will read.
2. Upgrade Relay. `relay serve` applies Relay's own migrations automatically;
   `relay migrate` remains available for pre-deployment jobs. Relay does not
   migrate application system databases.
3. Review every `data_plane` entry in `relay.yaml`:
   - For an application below schema 123, remove its `data_plane` entry and keep
     the application in Attached mode if its SDK still contains the connector. A
     post-split release below the floor is not supported by either mode.
   - For an application at schema 123 through 125, choose `integration: direct`
     or `integration: hybrid` and set `mode: read` first. Switch to
     `mode: read-write` only when the operator needs lifecycle mutations,
     schedule changes, or recovery.
   - Use `connection_string_from` or an environment expansion for the
     credential. Do not commit the database URL.
4. `relay apply -f relay.yaml --database-url "$RELAY_DATABASE_URL"` provisions
   control-plane records such as the organisation, applications, alert rules,
   and API keys, but it does not register data-plane connections. Start or
   restart `relay serve` with `RELAY_CONFIG` pointing at the manifest; the
   server registers each non-Attached entry and verifies the application schema.
   If initialization fails, check the log for
   `failed eager data-plane client initialization` or
   `failed lazy data-plane client initialization`; Relay keeps the application
   configured and retries after a short backoff. Then verify one workflow read.
5. For Direct applications that need automatic recovery, set
   `RELAY_INTERNAL_SECRET` on Relay and run `relay probe` beside the application.
   An operator-provided liveness signal is planned but has no published contract
   yet; do not plan on it. Without a signal, Relay observes and controls but does
   not detect a dead executor.
6. Test one safe mutation and one recovery scenario in staging before production
   use.

**Rollback**

The application system database is not changed by Relay migrations. If the
Relay binary must be rolled back, follow the snapshot and `relay migrate down`
procedures below; `migrate force` is for clearing a dirty migration state, not a
routine rollback. A Relay binary cannot run against a newer Relay schema, so a
database restore may be the safer path after a failed upgrade. Workflow
mutations or recovery already performed through Direct mode remain in the
application database; removing the data-plane entry does not undo them.

See [Migrating from Conductor](from-conductor.md) for the application cutover
and [Direct mode](direct.md) for the full configuration and operation list.

## Automatic migration on startup

Relay embeds database schema migrations within the binary. When starting Relay with
`relay serve`, the server automatically applies any pending up-migrations before
opening network listeners:

```bash
./bin/relay serve
```

For automated deployments, staging validation, or dedicated migration jobs, migrations
can alternatively be applied explicitly:

```bash
./bin/relay migrate
# or explicitly apply pending up-migrations:
./bin/relay migrate up
```

Running explicit migrations is optional in single-instance deployments, but recommended
in automated CI/CD pipelines before routing production traffic to new binaries. When running
in an environment where migrations are managed externally or applied beforehand, automatic
startup migrations can be skipped via `--skip-migrations`:

```bash
./bin/relay serve --skip-migrations
```

## Rolling upgrades and schema compatibility

When deploying Relay in a high-availability cluster with multiple instances sharing a
Postgres database, follow an expand/contract schema evolution model:

1. **Additive schema changes**: All migrations must be backwards-compatible with currently
   running server instances. Existing columns and tables must remain readable.
2. **Version forward compatibility**: Newer server instances can start against existing schemas
   and apply migrations.
3. **Version backward incompatibility**: Older Relay binaries will fail to start if the database
   schema is at a version higher than the binary's embedded migrations. The startup fails with:
   ```
   no migration found for version <N>
   ```
4. **Upgrade order**: Apply migrations or start the new Relay instance first, then roll out
   updated binaries across the instance pool.

## Rollback procedure and migration management

Relay embeds paired `.down.sql` migrations for schema definitions. The Relay CLI provides
subcommands to inspect migration status, roll back versions, and recover from dirty states:

```bash
# Check current database schema version and dirty status
./bin/relay migrate status

# Roll back the most recent schema migration
./bin/relay migrate down

# Reset dirty state or force a specific version after manual refinement
./bin/relay migrate force --version <N> --confirm
```

Supported rollback procedures:

1. **Database restore**: The primary and recommended rollback procedure for schema changes
   is restoring the database from a snapshot taken immediately prior to migration.
2. **CLI down-migration**: Apply `.down.sql` migrations using `./bin/relay migrate down`.
3. **External migration tool**: If rolling back via external tooling is required, operators
   may apply the corresponding `.down.sql` files using the external `golang-migrate` CLI tool
   pointing at the database URL.

### Dirty migration state (`ErrDirty`)

If a migration fails mid-execution (for example due to network termination or constraint violation),
the database driver marks `schema_migrations.dirty = true`. When dirty, Relay refuses to start
or apply further migrations:

```text
migration failed: Dirty database version <N>. Fix and force version.
```

Operators must manually inspect the database state, resolve the underlying schema error, and
reset the dirty flag using `./bin/relay migrate force --version <N> --confirm` before restarting Relay.

## Storage engine architecture

Relay features a dual-engine storage architecture defined in ADR 0011, supporting both enterprise clustered deployments and zero-dependency embedded workflows.

### Flagship PostgreSQL

PostgreSQL remains Relay's flagship, tier-1 storage engine for production deployments:

- **Multi-node HA**: Full clustering with coordinator instance heartbeats, active lease tracking, and automatic orphan executor adoption.
- **Concurrency control**: Native row-level locking (`FOR UPDATE SKIP LOCKED`) and transactional guarantees.
- **Production scale**: Tested against PostgreSQL 15, 16, and 17.

To use PostgreSQL, supply a `postgres://` or `postgresql://` connection URL via `--database-url` or the `RELAY_DATABASE_URL` environment variable.

### Embedded SQLite

For single-process deployments, edge nodes, local development, and CI pipelines, Relay provides an embedded pure-Go SQLite engine:

- **Zero CGO**: Statically compiled with `modernc.org/sqlite`. The binary requires no external dynamic C libraries or GCC toolchains.
- **Zero configuration**: Run `relay serve --embedded` to initialize and migrate `./data/relay.db` automatically.
- **Standalone mode**: In embedded mode, the HA manager operates in standalone mode. Multi-instance heartbeat loops and lease contention cycles are suppressed, while orphan executors are reconciled immediately at startup.
- **WAL mode**: Embedded databases are opened with Write-Ahead Logging (`PRAGMA journal_mode = WAL`) and busy timeouts for concurrent readers.
- **Protocol conformance**: Embedded SQLite implements the complete `gen.Querier` contract and passes all eight DBOS Conductor conformance batteries.

### Scale-out path: Turso Database

For operators who start with SQLite and later require distributed replication or multi-writer concurrency without migrating schemas to PostgreSQL:

- **Turso Database**: Operates on SQLite-compatible storage with concurrent multi-writer MVCC architecture.
- **Replication**: Turso provides distributed multi-region edge replication.
- **Conformance**: Relay's SQLite migrations and queries avoid engine-specific extensions, keeping the dialect compatible with standard SQLite and Turso deployments.
