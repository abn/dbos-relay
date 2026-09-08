-- name: CreateOrganisation :one
INSERT INTO organisations (name) VALUES ($1) RETURNING *;

-- name: GetOrganisationByName :one
SELECT * FROM organisations WHERE name = $1;
