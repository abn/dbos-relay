CREATE TABLE recovery_dispatches (
    id                 text PRIMARY KEY,
    application_id     text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    dead_executor_id   text NOT NULL,
    target_executor_id text NOT NULL,
    dispatched_at      text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    success            integer NOT NULL DEFAULT 1
);

CREATE INDEX recovery_dispatches_app_dead_idx ON recovery_dispatches (application_id, dead_executor_id, dispatched_at);
