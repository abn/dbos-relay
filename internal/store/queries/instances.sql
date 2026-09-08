-- name: UpsertInstance :one
INSERT INTO instances (id, advertise_address, port, started_at, heartbeat_at)
VALUES ($1, $2, $3, now(), now())
ON CONFLICT (id) DO UPDATE SET
    advertise_address = EXCLUDED.advertise_address,
    port = EXCLUDED.port,
    heartbeat_at = now()
RETURNING *;

-- name: HeartbeatInstance :exec
UPDATE instances
SET heartbeat_at = now()
WHERE id = $1;

-- name: GetInstance :one
SELECT * FROM instances
WHERE id = $1;

-- name: ListHealthyInstances :many
SELECT * FROM instances
WHERE heartbeat_at > $1
ORDER BY started_at ASC;

-- name: DeleteInstance :exec
DELETE FROM instances
WHERE id = $1;

-- name: DeleteStaleInstances :execrows
DELETE FROM instances
WHERE heartbeat_at < $1;
