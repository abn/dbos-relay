-- name: CreateApplication :one
INSERT INTO applications (organisation_id, name, settings) VALUES ($1, $2, $3) RETURNING *;

-- name: GetApplicationByName :one
SELECT * FROM applications WHERE organisation_id = $1 AND name = $2;

-- name: GetApplicationByID :one
SELECT * FROM applications WHERE id = $1;

-- name: ListApplicationsByOrganisation :many
SELECT * FROM applications
WHERE organisation_id = $1
ORDER BY created_at DESC;

-- name: UpsertApplication :one
INSERT INTO applications (organisation_id, name, settings)
VALUES ($1, $2, $3)
ON CONFLICT (organisation_id, name) DO UPDATE SET
    settings = EXCLUDED.settings
RETURNING *;

-- name: UpdateApplicationSettings :one
UPDATE applications
SET settings = $3
WHERE organisation_id = $1 AND name = $2
RETURNING *;

-- name: DeleteApplication :one
DELETE FROM applications
WHERE organisation_id = $1 AND name = $2
RETURNING *;
