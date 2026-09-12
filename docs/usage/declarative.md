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
    stuck_sla_secs: 900
    executor_timeout_secs: 60
  - name: "payment-gateway"
    description: "Payment processing"
    stuck_sla_secs: 600

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
  # Sensitive database credentials use environment variable expansion or secret file indirection
  order-service:
    connection_url: "${ORDER_SERVICE_DB_URL}"
    mode: "read"
    statement_timeout_secs: 10
    max_connections: 5

  payment-gateway:
    connection_string_from:
      env: "PAYMENT_DB_URL"
      # Or secret file:
      # file: "/var/run/secrets/db_url"
    mode: "read"
    statement_timeout_secs: 10
    max_connections: 5
```

## Schema reference

The declarative manifest schema supports the following fields:

* `version`: Schema version string (currently `"1"`).
* `organisation`: The target organisation name.
* `applications`: List of application definitions:
  * `name`: Unique application name (required).
  * `description`: Freeform text description.
  * `stuck_sla_secs`: SLA threshold in seconds for alerting on stuck workflows.
  * `executor_timeout_secs`: Liveness timeout in seconds before an executor is declared dead.
* `alert_rules`: List of alerting rule specifications:
  * `app`: Target application name.
  * `rule_type`: Rule classifier (`UnresponsiveApplication`, `RecoveryFlapping`, `StrandedVersion`).
  * `receiving_app`: Application handling notification dispatch.
  * `min_interval_secs`: Notification rate limit interval in seconds.
  * `metadata`: Type-specific configuration dictionary (e.g. destinations, thresholds).
* `data_plane`: Map of application names to data-plane configurations:
  * `connection_url`: Database URL with optional `${ENV_VAR}` expansion.
  * `connection_string_from`: Secret source indirection with `env` or `file`.
  * `mode`: Access mode (`read` or `read-write`, defaults to `read`).
  * `statement_timeout_secs`: Statement execution timeout.
  * `max_connections`: Connection pool size.

## Secret indirection

Never commit raw database credentials in `relay.yaml`. Relay provides two mechanisms for credential indirection:
1. Environment variable expansion in `connection_url`: `${DB_URL}` is resolved from the environment at startup.
2. The `connection_string_from` object: configure `env` with the name of an environment variable or `file` with the absolute path to a secret file.

See `examples/relay.yaml` for a reference configuration.

## Reviewing changes with diff

The `relay diff` command compares the local manifest against the current Relay store
and prints a reconciliation plan without modifying the database:

```bash
relay diff -f relay.yaml --database-url "$RELAY_DATABASE_URL"
```

Flags:
* `-f, --file`: Path to `relay.yaml` manifest file (required).
* `--database-url`: PostgreSQL database URL (defaults to `RELAY_DATABASE_URL` environment variable).

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
relay apply -f relay.yaml --database-url "$RELAY_DATABASE_URL" --env-out deploy/.env
```

Flags:
* `-f, --file`: Path to `relay.yaml` configuration file (required).
* `--database-url`: PostgreSQL database URL (defaults to `RELAY_DATABASE_URL` environment variable).
* `--env-out`: Optional path to write generated environment variables (e.g. `RELAY_API_KEY`).

Actions performed during apply:
1. Creates the organisation if it does not already exist.
2. Creates any missing applications. Existing applications remain unchanged.
3. Configures alerting rules for each application.
4. Mints a default conductor API key (`default-conductor-key`) when applications are created.
5. If `--env-out` is specified, writes the generated keys to the target environment file. As a security safeguard, `relay apply` inspects git repositories and refuses to write secrets to an unignored path (`refusing to write secrets to unignored path ... inside git repository`).
6. Reports a summary of changes applied.
