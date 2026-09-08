---
type: HowTo
title: Declarative operations
description: Fleet configuration using relay.yaml, relay diff, and relay apply.
status: draft
---

# Declarative operations

Relay supports declarative fleet management through YAML manifests (`relay.yaml`).
The declarative workflow enables automated, reproducible provisioning of organisations,
applications, alerting rules, and data-plane connections.

## Manifest format

A `relay.yaml` file specifies the desired state for a Relay instance:

```yaml
version: "1"

organisation: "production"

applications:
  - name: "order-service"
    description: "Order fulfillment service"
  - name: "payment-gateway"
    description: "Payment processing"

alert_rules:
  - app: "order-service"
    rule_type: "UnresponsiveApplication"
    receiving_app: "order-service"
    min_interval_secs: 60
    metadata:
      destinations:
        - type: "slack"
          url: "https://hooks.slack.com/services/..."
  - app: "order-service"
    rule_type: "RecoveryFlapping"
    min_interval_secs: 300
    metadata:
      threshold: 3

data_plane:
  order-service:
    connection_url: "postgres://dbos_read:secret@db.internal:5432/order_system_db"
    mode: "read"
    statement_timeout_secs: 10
    max_connections: 5
```

## Reviewing changes with diff

The `relay diff` command compares the local manifest against the current Relay store
and prints a reconciliation plan without modifying the database:

```bash
relay diff -f relay.yaml --database-url "$RELAY_DATABASE_URL"
```

Sample output:

```text
  [UNCHANGED] Organisation (production)
+ [CREATE] Application (order-service)
+ [CREATE] AlertRule (order-service/UnresponsiveApplication)
+ [CREATE] AlertRule (order-service/RecoveryFlapping)
  [UNCHANGED] DataPlane (order-service): mode=read
Plan: 3 to create, 0 to delete, 2 unchanged.
```

## Applying configuration

The `relay apply` command reconciles the store to match the manifest:

```bash
relay apply -f relay.yaml --database-url "$RELAY_DATABASE_URL"
```

Actions performed during apply:

1. Creates the organisation if it does not already exist.
2. Creates any missing applications. Existing applications remain unchanged.
3. Configures alerting rules for each application.
4. Reports a summary of changes applied.
