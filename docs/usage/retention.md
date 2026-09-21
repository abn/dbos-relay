---
type: Guide
title: Workflow retention
description: Configuring workflow history retention and global timeouts.
status: draft
---

# Workflow retention

Retention controls how long completed workflow history is kept in an
application's system database. Relay stores each application's thresholds
and dispatches them to a healthy executor, which enforces them against its
own system database. Relay never deletes application rows itself.

## Configuring thresholds

Set thresholds with `PATCH /v2/orgs/{orgName}/apps/{appName}` or
`dbosctl app update`:

* `--gc-rows-threshold`: keep history for the N most recently completed
  workflows; older completed workflows are garbage-collected.
* `--gc-time-threshold-ms`: delete history of workflows completed more
  than this many milliseconds ago.
* `--global-timeout-ms`: cancel any workflow still incomplete this many
  milliseconds after it was created.

Only completed workflows (`SUCCESS`, `ERROR`, `CANCELLED`,
`MAX_RECOVERY_ATTEMPTS_EXCEEDED`) are ever deleted. Running, enqueued, or
delayed workflows are never touched. Deleting a workflow's history also
deletes its steps, inputs, outputs, messages, events, and streams.

## Value bounds

The server rejects nonsense thresholds with `400 Bad Request` (and
records a failure audit entry):

* Retention rows, retention time, and global timeout must not be
  negative. Zero is allowed and means keep nothing: a zero time
  threshold dispatches a cutoff of now, deleting all completed history.
* The executor timeout must be positive. The server rejects zero and
  negative values explicitly instead of silently falling back to the
  default on read.

The dashboard blocks negative numbers client-side before sending; a
zero executor timeout is rejected by the server with a failure entry.

## Shared system databases

When multiple applications share one system database, retention applies to
the whole database: the most restrictive policy among the sharing
applications wins for every application on it. Configure identical
retention policies for all applications sharing a database
([upstream retention docs](https://docs.dbos.dev/production/retention),
confirmed 2026-09-21).
The global timeout applies only to workflows owned by the application it
is configured on.

## Dashboard status

Retention is managed through the API, `dbosctl`, and the dashboard
Settings view (per-application retention rows and hours, global timeout,
executor timeout, and private mode). The Settings form sends only filled
fields; blank fields leave current values unchanged.

## Verification status

Threshold storage and executor dispatch are covered by unit tests. Live
enforcement runs inside the SDKs; end-to-end certification with real SDK
executors is tracked under `make verify-sdk` and `make verify-live`
(see [Conformance testing](conformance.md)).
