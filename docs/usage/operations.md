---
type: Guide
title: Operations and migrations
description: Operational procedures for Relay schema migrations, rolling upgrades, and rollbacks.
status: stable
---

# Operations and migrations

This document covers operational procedures for database migrations, rolling upgrades,
and schema rollback strategies in Relay deployments.

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
./bin/relay migrate force <version>
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
reset the dirty flag using `./bin/relay migrate force <version>` before restarting Relay.

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
