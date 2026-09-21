-- name: UpsertAutoscalingPolicy :one
INSERT INTO autoscaling_policies (application_id, queue, max_old_versions, max_executors_old, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (application_id) DO UPDATE SET
    queue = EXCLUDED.queue,
    max_old_versions = EXCLUDED.max_old_versions,
    max_executors_old = EXCLUDED.max_executors_old,
    updated_at = now()
RETURNING *;

-- name: GetAutoscalingPolicy :one
SELECT * FROM autoscaling_policies
WHERE application_id = $1;

-- name: DeleteAutoscalingPolicy :execrows
DELETE FROM autoscaling_policies
WHERE application_id = $1;
