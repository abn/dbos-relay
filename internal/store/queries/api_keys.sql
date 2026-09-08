-- name: CreateAPIKey :one
INSERT INTO api_keys (organisation_id, name, lookup, key_hash, application_names, permissions)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAPIKeyByLookup :one
SELECT * FROM api_keys
WHERE lookup = $1 AND revoked_at IS NULL;

-- name: ListAPIKeys :many
SELECT * FROM api_keys
WHERE organisation_id = $1
ORDER BY created_at DESC;

-- name: RevokeAPIKey :one
UPDATE api_keys
SET revoked_at = now()
WHERE id = $1 AND organisation_id = $2
RETURNING *;

-- name: TouchAPIKeyLastUsed :exec
UPDATE api_keys
SET last_used_at = now()
WHERE id = $1;
