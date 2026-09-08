-- name: UpsertExecutor :one
INSERT INTO executors (
    application_id,
    executor_id,
    application_version,
    hostname,
    metadata,
    status,
    owner_instance_id,
    lease_expires_at,
    connected_at,
    last_seen_at,
    disconnected_at
) VALUES (
    $1, $2, $3, $4, $5, 'connected', $6, $7, now(), now(), NULL
)
ON CONFLICT (application_id, executor_id) DO UPDATE SET
    application_version = EXCLUDED.application_version,
    hostname = EXCLUDED.hostname,
    metadata = EXCLUDED.metadata,
    status = 'connected',
    owner_instance_id = EXCLUDED.owner_instance_id,
    lease_expires_at = EXCLUDED.lease_expires_at,
    last_seen_at = now(),
    disconnected_at = NULL
RETURNING *;

-- name: TouchExecutorLastSeen :exec
UPDATE executors
SET last_seen_at = now(),
    lease_expires_at = $3
WHERE application_id = $1 AND executor_id = $2;

-- name: DisconnectExecutor :one
UPDATE executors
SET status = 'disconnected',
    disconnected_at = now(),
    owner_instance_id = NULL,
    lease_expires_at = NULL
WHERE application_id = $1 AND executor_id = $2
RETURNING *;

-- name: ListExecutorsByApplication :many
SELECT * FROM executors
WHERE application_id = $1
ORDER BY connected_at DESC;

-- name: ListConnectedExecutorsByApplication :many
SELECT * FROM executors
WHERE application_id = $1 AND status = 'connected'
ORDER BY last_seen_at DESC;

-- name: GetExecutorByID :one
SELECT * FROM executors
WHERE application_id = $1 AND executor_id = $2;

-- name: ReapExpiredExecutors :execrows
UPDATE executors
SET status = 'dead'
WHERE status = 'disconnected' AND disconnected_at < $1;
