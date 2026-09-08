-- name: CreateApplication :one
INSERT INTO applications (organisation_id, name, settings) VALUES ($1, $2, $3) RETURNING *;

-- name: GetApplicationByName :one
SELECT * FROM applications WHERE organisation_id = $1 AND name = $2;
