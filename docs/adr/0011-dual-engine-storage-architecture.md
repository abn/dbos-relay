---
type: Decision
title: ADR 0011 - Dual-engine storage architecture and embedded mode
description: Storage architecture establishing PostgreSQL as flagship production engine, pure-Go SQLite for embedded mode, Turso MVCC for scale-out, and future adoption of Turso PostgreSQL frontend.
status: accepted
---

# ADR 0011 - Dual-engine storage architecture and embedded mode

## Status

Accepted

## Context

Relay currently requires a dedicated PostgreSQL database (`RELAY_DATABASE_URL="postgres://..."`). Its persistence layer (`internal/store`) relies on `pgx/v5` and PostgreSQL-specific features: `uuid` with `gen_random_uuid()`, `jsonb` indexing, array types (`text[]`), regex `CHECK` constraints, native enum types (`executor_status`), and row-level lease fencing (`SKIP LOCKED` and atomic lease updates in `internal/ha`).

While PostgreSQL aligns with DBOS Transact applications (which store application workflow state in PostgreSQL) and provides robust multi-node high availability (HA), it introduces operational friction for developers evaluating Relay locally, CI test suites, and single-node edge deployments that desire a self-contained, zero-dependency binary.

We evaluated potential embedded database engines, including PGlite (WASM Postgres), embedded SQLite (pure Go), and Turso Database (Rust rewrite of SQLite with multi-version concurrency control).

## Decision

We record the following architectural decisions:

1. **Dual-engine storage abstraction**: Relay will adopt a dual-engine storage model:
   - **PostgreSQL as flagship**: PostgreSQL is the normative, production-grade engine for all multi-node deployments, high availability clusters, and enterprise fleets.
   - **SQLite as embedded engine**: A pure-Go SQLite driver (`modernc.org/sqlite` or `ncruces/go-sqlite3`) will provide an in-process, zero-CGO embedded mode (e.g., `--embedded` or `RELAY_DATABASE_URL="sqlite://relay.db"`).
2. **Conformance and capability priority**: When trade-offs arise between PostgreSQL capabilities and SQLite dialect limitations, **conformance to the DBOS Conductor protocol and PostgreSQL flagship capabilities will always take priority**. Relay will not compromise or water down PostgreSQL features (such as distributed lease fencing, JSONB indexing, or transactional consistency) to fit SQLite constraints. Where SQLite lacks native types (e.g. arrays or UUIDs), the SQLite adapter will polyfill or adapt them without degrading the PostgreSQL path.
3. **Embedded mode operational boundaries**: Embedded SQLite is explicitly bounded to single-instance and edge deployments. In embedded mode, multi-node HA coordination (distributed lease heartbeats and peer orphan adoption) is bypassed.
4. **Turso MVCC recommendation for SQLite scale-out**: For deployments following the SQLite path that outgrow single-writer throughput or require distributed read replicas, **Turso Database is the recommended scale-out target**. Turso's concurrent-write engine (`PRAGMA journal_mode = 'mvcc'`) and replication capabilities eliminate SQLite's single-writer bottleneck without requiring migration to stateful PostgreSQL management.
5. **Future adoption of Turso PostgreSQL frontend**: Turso is developing an in-tree PostgreSQL frontend (wire protocol and SQL translator). When that capability matures to support Relay's full query dialect and binary encoding requirements, Relay will support Turso transparently over standard `postgres://` connections with zero code changes.

## Consequences

- **Single-binary developer experience**: Users can run `./relay serve --embedded` out of the box with zero external dependencies and zero Docker requirements.
- **Uncompromised production HA**: High-availability multi-instance Relay deployments continue to rely on the proven, rock-solid PostgreSQL engine.
- **Clear graduation path**: SQLite users have a documented scale-out route via Turso MVCC, while standard production deployments continue on managed PostgreSQL (AWS RDS, GCP Cloud SQL, Supabase, Neon).
- **Engineering scope**: Implementing this decision requires formalizing an engine-agnostic store interface in `internal/store`, maintaining dual-engine schema migrations, and configuring `sqlc` to generate queries for both PostgreSQL and SQLite dialects.
