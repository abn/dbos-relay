CREATE INDEX IF NOT EXISTS organisation_members_user_idx ON organisation_members (user_id, created_at);
CREATE INDEX IF NOT EXISTS alerting_rules_receiving_app_idx ON alerting_rules (receiving_application_id);
CREATE INDEX IF NOT EXISTS api_keys_org_created_idx ON api_keys (organisation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS domain_claims_org_domain_idx ON domain_claims (organisation_id, domain);
CREATE INDEX IF NOT EXISTS audit_logs_user_idx ON audit_logs (user_id);
CREATE INDEX IF NOT EXISTS instances_heartbeat_idx ON instances (heartbeat_at);

CREATE TRIGGER IF NOT EXISTS trg_check_organisation_member_role_insert
BEFORE INSERT ON organisation_members
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM roles
    WHERE name = NEW.role_name
      AND (organisation_id = NEW.organisation_id OR organisation_id IS NULL)
)
BEGIN
    SELECT RAISE(ABORT, 'role does not exist in organisation or globally');
END;

CREATE TRIGGER IF NOT EXISTS trg_check_organisation_member_role_update
BEFORE UPDATE OF role_name, organisation_id ON organisation_members
FOR EACH ROW
WHEN NOT EXISTS (
    SELECT 1 FROM roles
    WHERE name = NEW.role_name
      AND (organisation_id = NEW.organisation_id OR organisation_id IS NULL)
)
BEGIN
    SELECT RAISE(ABORT, 'role does not exist in organisation or globally');
END;

CREATE TRIGGER IF NOT EXISTS trg_check_role_deletion
BEFORE DELETE ON roles
FOR EACH ROW
WHEN EXISTS (
    SELECT 1 FROM organisation_members
    WHERE role_name = OLD.name
      AND (organisation_id = OLD.organisation_id OR OLD.organisation_id IS NULL)
)
BEGIN
    SELECT RAISE(ABORT, 'cannot delete role because it is still referenced by organisation members');
END;
