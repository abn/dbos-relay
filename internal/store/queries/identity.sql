-- name: CreateUser :one
INSERT INTO users (subject, username, email, is_admin)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpsertUser :one
INSERT INTO users (subject, username, email, is_admin)
VALUES ($1, $2, $3, $4)
ON CONFLICT (subject) DO UPDATE SET
    username = EXCLUDED.username,
    email = EXCLUDED.email
RETURNING *;

-- name: GetUserBySubject :one
SELECT * FROM users
WHERE subject = $1;

-- name: GetUserByUsername :one
SELECT * FROM users
WHERE username = $1;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: ListMembersByOrganisation :many
SELECT u.id AS user_id, u.username, u.email, om.role_name, om.created_at
FROM organisation_members om
JOIN users u ON om.user_id = u.id
WHERE om.organisation_id = $1
ORDER BY om.created_at ASC;

-- name: GetMember :one
SELECT u.id AS user_id, u.username, u.email, om.role_name, om.created_at
FROM organisation_members om
JOIN users u ON om.user_id = u.id
WHERE om.organisation_id = $1 AND u.username = $2;

-- name: UpsertMemberRole :one
INSERT INTO organisation_members (organisation_id, user_id, role_name)
VALUES ($1, $2, $3)
ON CONFLICT (organisation_id, user_id) DO UPDATE SET
    role_name = EXCLUDED.role_name
RETURNING *;

-- name: RemoveMember :one
DELETE FROM organisation_members
WHERE organisation_id = $1 AND user_id = $2
RETURNING *;

-- name: GetUserPrimaryOrganisation :one
SELECT o.id, o.name, o.created_at, om.role_name
FROM organisations o
JOIN organisation_members om ON o.id = om.organisation_id
WHERE om.user_id = $1
ORDER BY om.created_at ASC
LIMIT 1;

-- name: ListRoles :many
SELECT * FROM roles
WHERE organisation_id = $1 OR organisation_id IS NULL
ORDER BY is_global DESC, name ASC;

-- name: GetRole :one
SELECT * FROM roles
WHERE (organisation_id = $1 OR organisation_id IS NULL) AND name = $2
LIMIT 1;

-- name: CreateRole :one
INSERT INTO roles (organisation_id, name, permissions, is_global)
VALUES ($1, $2, $3, false)
RETURNING *;

-- name: DeleteRole :one
DELETE FROM roles
WHERE organisation_id = $1 AND name = $2 AND is_global = false
RETURNING *;

-- name: ListDomainClaims :many
SELECT * FROM domain_claims
WHERE organisation_id = $1
ORDER BY domain ASC;

-- name: GetDomainClaim :one
SELECT * FROM domain_claims
WHERE domain = $1;

-- name: CreateDomainClaim :one
INSERT INTO domain_claims (organisation_id, domain)
VALUES ($1, $2)
RETURNING *;

-- name: DeleteDomainClaim :one
DELETE FROM domain_claims
WHERE organisation_id = $1 AND domain = $2
RETURNING *;

-- name: CreateAuditLog :one
INSERT INTO audit_logs (organisation_id, user_id, username, action, details)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE organisation_id = sqlc.arg('organisation_id')
AND (sqlc.narg('start_time')::timestamptz IS NULL OR created_at >= sqlc.narg('start_time'))
AND (sqlc.narg('end_time')::timestamptz IS NULL OR created_at <= sqlc.narg('end_time'))
AND (sqlc.narg('operation')::text IS NULL OR action = sqlc.narg('operation'))
AND (sqlc.narg('subject')::text IS NULL OR username = sqlc.narg('subject')
    OR details->>'subject_display' = sqlc.narg('subject')
    OR details->>'subject_id' = sqlc.narg('subject'))
AND (sqlc.narg('target')::text IS NULL OR details->>'target_id' = sqlc.narg('target'))
ORDER BY created_at DESC
LIMIT sqlc.arg('limit')::bigint OFFSET sqlc.arg('offset')::bigint;

-- name: DeleteExpiredAuditLogs :execrows
DELETE FROM audit_logs
WHERE organisation_id = sqlc.arg('organisation_id')
AND created_at < sqlc.arg('cutoff');
