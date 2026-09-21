CREATE TABLE autoscaling_policies (
    application_id           text PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    queue                    text NOT NULL,
    max_old_versions         integer,
    max_executors_old        integer,
    created_at               text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at               text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
