-- name: CreateOrganisation :one
INSERT INTO organisations (name) VALUES ($1) RETURNING *;

-- name: GetOrganisationByName :one
SELECT * FROM organisations WHERE name = $1;

-- name: GetOrganisationByID :one
SELECT * FROM organisations WHERE id = $1;

-- name: UpsertOrganisation :one
INSERT INTO organisations (name)
VALUES ($1)
ON CONFLICT (name) DO UPDATE SET
    name = EXCLUDED.name
RETURNING *;

-- name: ListAllOrganisations :many
SELECT * FROM organisations ORDER BY name;

-- name: UpdateOrganisation :one
UPDATE organisations SET
    name = COALESCE(sqlc.narg('name'), name),
    audit_log_retention_days = COALESCE(sqlc.narg('audit_log_retention_days'), audit_log_retention_days)
WHERE id = sqlc.arg('id')
RETURNING *;
