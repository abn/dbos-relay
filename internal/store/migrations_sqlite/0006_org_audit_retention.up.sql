ALTER TABLE organisations
    ADD COLUMN audit_log_retention_days INTEGER NOT NULL DEFAULT 90
    CHECK (audit_log_retention_days >= 7 AND audit_log_retention_days <= 3650);
