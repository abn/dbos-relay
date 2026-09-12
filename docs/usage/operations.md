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
```

Running explicit migrations is optional in single-instance deployments, but recommended
in automated CI/CD pipelines before routing production traffic to new binaries.

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

## Rollback procedure

Relay embeds paired `.down.sql` migrations for schema definitions, but the Relay CLI deliberately
exposes no down-migration command to prevent accidental data loss in production.

Supported rollback procedures:

1. **Database restore**: The primary and recommended rollback procedure for schema changes
   is restoring the database from a snapshot taken immediately prior to migration.
2. **External migration tool**: If rolling back without database restore is required, operators
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
reset the dirty flag using `golang-migrate force <version>` or by updating `schema_migrations`
before restarting Relay.
