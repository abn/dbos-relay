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

```
migration failed: Dirty database version <N>. Fix and force version.
```

Operators must manually inspect the database state, resolve the underlying schema error, and
reset the dirty flag using `./bin/relay migrate force <version>` before restarting Relay.
