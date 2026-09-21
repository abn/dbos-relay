---
type: Guide
title: Autoscaling
description: Queue-driven executor recommendations for external scalers.
status: draft
---

# Autoscaling

Relay computes how many executors each application version needs from
queue backlog, so autoscalers like KEDA can size deployments per version,
drain old versions to zero, and drive rollouts. Relay recommends counts;
it never scales anything itself. See [ADR
0012](../adr/0012-autoscaling-policy-support.md) for the design.

This follows the upstream contract
([upstream autoscaling docs](https://docs.dbos.dev/production/autoscaling),
confirmed 2026-09-21).

## Attaching a policy

A policy names the queue whose backlog drives the executor count:

```bash
curl -X PUT "$RELAY/v2/orgs/my-org/apps/my-app/autoscaling-policy" \
  -H "Authorization: Bearer $RELAY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"queue": "orders"}'
```

Relay validates the queue against a running executor before storing the
policy. The queue must exist, must not be partitioned, and must have a
worker concurrency set; otherwise the write is rejected with 400. The
optional `rollout` section caps old versions during rollouts:

```json
{
  "queue": "orders",
  "rollout": {
    "maxOldApplicationVersions": 2,
    "maxExecutorsForOldApplicationVersions": 1
  }
}
```

`GET` the same path reads the stored policy (404 when none is set);
`DELETE` turns autoscaling off (204 even when no policy was stored; the
audit entry records whether one was removed). Setting and deleting require
`application.write`.

## Reading recommendations

Per version (for a KEDA scaler polling one deployment):

```bash
curl -H "Authorization: Bearer $RELAY_API_KEY" \
  "$RELAY/v2/orgs/my-org/apps/my-app/autoscale/versions/latest"
```

All versions at once (for an operator owning every deployment):

```bash
curl -H "Authorization: Bearer $RELAY_API_KEY" \
  "$RELAY/v2/orgs/my-org/apps/my-app/autoscale"
```

Each entry reports `applicationVersion`, `isLatest`,
`desiredExecutors`, `queueName`, `queueDepth`, and `observedAt`.

## How the count is computed

Backlog is the `ENQUEUED` plus `PENDING` depth on the policy queue,
counted per application version from executor-reported listings:

```
desiredExecutors = ceil(queueDepth / workerConcurrency)
```

A queue-level concurrency limit additionally caps the recommendation at
`ceil(concurrency / workerConcurrency)`. The latest version is always
reported with at least one executor; an old version with no remaining
work is omitted, which signals that its deployment can be deleted.
`maxExecutorsForOldApplicationVersions` caps every old version, and at
most `maxOldApplicationVersions` old versions are listed (default 0:
latest only). Old versions are ordered most recently connected first.
The latest version resolves from the recorded latest version when set,
otherwise from the most recently connected executor, otherwise from the
lexicographic maximum of known versions. When no version is known at all
(no recorded latest, no executors, no backlog), the all-versions endpoint
returns an empty list rather than inventing a version; the single-version
endpoint still honors an explicitly recorded latest at its minimum of one.

## KEDA example

One `ScaledObject` per version deployment, mapping the absolute count
one-to-one to replicas:

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: my-app-latest
spec:
  scaleTargetRef:
    name: my-app-v2
  minReplicaCount: 1
  maxReplicaCount: 32
  triggers:
    - type: metrics-api
      metadata:
        url: "https://relay.example.com/v2/orgs/my-org/apps/my-app/autoscale/versions/latest"
        valueLocation: "desiredExecutors"
        authMode: "bearer"
```

## Errors

| Status | Meaning |
| --- | --- |
| `400` | The policy names no queue, an unknown or partitioned queue, a queue without worker concurrency, or a negative rollout cap. |
| `404` | No autoscaling policy is stored, or the requested version was never registered. |
| `502` / `503` | No healthy executor answered the queue or backlog query. |
