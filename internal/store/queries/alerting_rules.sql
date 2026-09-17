-- name: CreateAlertingRule :one
INSERT INTO alerting_rules (
    application_id,
    receiving_application_id,
    rule_type,
    rule_metadata,
    min_interval_secs
) VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAlertingRule :one
SELECT * FROM alerting_rules
WHERE id = $1 AND application_id = $2;

-- name: ListAlertingRulesByApplication :many
SELECT * FROM alerting_rules
WHERE application_id = $1
ORDER BY created_at ASC;

-- name: DeleteAlertingRule :execrows
DELETE FROM alerting_rules
WHERE id = $1 AND application_id = $2;

-- name: TouchAlertRuleLastFired :exec
UPDATE alerting_rules
SET last_fired_at = now()
WHERE id = $1;

-- name: TouchAlertRuleLastFiredAtomic :one
UPDATE alerting_rules
SET last_fired_at = now()
WHERE id = $1
  AND application_id = $2
  AND (
    last_fired_at IS NULL
    OR min_interval_secs IS NULL
    OR min_interval_secs <= 0
    OR last_fired_at <= now() - (min_interval_secs * interval '1 second')
  )
RETURNING *;
