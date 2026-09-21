CREATE TABLE autoscaling_policies (
    application_id           uuid PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    queue                    text NOT NULL,
    max_old_versions         bigint,
    max_executors_old        bigint,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now()
);
