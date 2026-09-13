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
