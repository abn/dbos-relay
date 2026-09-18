CREATE TABLE alerting_rules (
    id                       text PRIMARY KEY,
    application_id           text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    receiving_application_id text NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    rule_type                text NOT NULL CHECK (rule_type IN ('WorkflowFailure', 'SlowQueue', 'UnresponsiveApplication', 'RecoveryFlapping', 'StrandedVersion')),
    rule_metadata            text NOT NULL DEFAULT '{}',
    min_interval_secs        integer,
    last_fired_at            text,
    created_at               text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX alerting_rules_app_idx ON alerting_rules (application_id);
