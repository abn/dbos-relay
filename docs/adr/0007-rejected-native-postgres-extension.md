---
type: Decision
title: ADR 0007 - Reject native Postgres extension
description: Reject compiling native extensions to run inside the application system database.
status: rejected
---

# ADR 0007 - Reject native Postgres extension

## Status

Rejected.

## Context

An option investigated for active system database monitoring, bloat telemetry,
and event push notifications was compiling a native PostgreSQL C/Rust/Zig
extension (e.g. using `pgrx` or `pgzx`) to run directly inside the application's
database cluster.

## Decision

Relay will not build, package, or require a native PostgreSQL extension.

The decision is based on three constraints:

1. **Managed cloud database incompatibility**: The majority of production DBOS
   deployments operate on managed database services (AWS Aurora/RDS, Google
   Cloud SQL, Supabase, Neon, Azure PostgreSQL). None of these platforms allow
   installing untrusted, unlisted third-party native shared library extensions.
2. **Blast radius and critical-path coupling**: In DBOS Transact, the database
   sits on the critical application transaction path. Running custom extension
   code inside the database process introduces crash and memory corruption
   risks to user databases.
3. **Inability to act on recovery**: An in-database extension lacks executor
   workflow definitions and cannot recover workflows on its own without active
   worker processes.

Any optional database objects Relay provides in the future (such as monitoring
views or helper functions) will ship as plain, unprivileged SQL definitions
applied idempotently via `relay apply`.

## Consequences

- Keeps database requirements standard and compatible with all managed Postgres
  providers.
- Restricts database interactions to standard client connection protocols.
