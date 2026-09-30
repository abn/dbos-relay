CREATE UNIQUE INDEX IF NOT EXISTS api_keys_org_name_active_idx
    ON api_keys (organisation_id, name)
    WHERE revoked_at IS NULL;
