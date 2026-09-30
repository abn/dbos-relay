-- name: CreateAPIKey :one
INSERT INTO api_keys (organisation_id, name, lookup, key_hash, application_names, permissions)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAPIKeyByLookup :one
SELECT * FROM api_keys
WHERE lookup = $1 AND revoked_at IS NULL;

-- name: GetAPIKeyByName :one
SELECT * FROM api_keys
WHERE organisation_id = $1 AND name = $2 AND revoked_at IS NULL;

-- name: RenameAPIKey :one
UPDATE api_keys
SET name = $3
WHERE api_keys.id = $1 AND api_keys.organisation_id = $2 AND api_keys.revoked_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM api_keys AS other
    WHERE other.organisation_id = $2
      AND other.name = $3
      AND other.revoked_at IS NULL
      AND other.id <> $1
  )
RETURNING *;

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
