CREATE TABLE alerting_rules (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id           uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    receiving_application_id uuid NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    rule_type                text NOT NULL CHECK (rule_type IN ('WorkflowFailure', 'SlowQueue', 'UnresponsiveApplication')),
    rule_metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,
    min_interval_secs        integer,
    last_fired_at            timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX alerting_rules_app_idx ON alerting_rules (application_id);
