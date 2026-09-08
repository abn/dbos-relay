-- name: RecordRecoveryDispatch :one
INSERT INTO recovery_dispatches (
    application_id,
    dead_executor_id,
    target_executor_id,
    dispatched_at,
    success
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: CountRecentRecoveryDispatches :one
SELECT count(*) FROM recovery_dispatches
WHERE application_id = $1
  AND dead_executor_id = $2
  AND dispatched_at >= $3;

-- name: ListRecentRecoveryDispatches :many
SELECT * FROM recovery_dispatches
WHERE application_id = $1
  AND dispatched_at >= $2
ORDER BY dispatched_at DESC
LIMIT $3;
