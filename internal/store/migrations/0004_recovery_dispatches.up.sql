CREATE TABLE recovery_dispatches (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id     uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    dead_executor_id   text NOT NULL,
    target_executor_id text NOT NULL,
    dispatched_at      timestamptz NOT NULL DEFAULT now(),
    success            boolean NOT NULL DEFAULT true
);

CREATE INDEX recovery_dispatches_app_dead_idx ON recovery_dispatches (application_id, dead_executor_id, dispatched_at);
