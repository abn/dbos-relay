CREATE INDEX IF NOT EXISTS organisation_members_user_idx ON organisation_members (user_id, created_at);
CREATE INDEX IF NOT EXISTS alerting_rules_receiving_app_idx ON alerting_rules (receiving_application_id);
CREATE INDEX IF NOT EXISTS api_keys_org_created_idx ON api_keys (organisation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS domain_claims_org_domain_idx ON domain_claims (organisation_id, domain);
CREATE INDEX IF NOT EXISTS audit_logs_user_idx ON audit_logs (user_id);
CREATE INDEX IF NOT EXISTS instances_heartbeat_idx ON instances (heartbeat_at);

CREATE OR REPLACE FUNCTION check_organisation_member_role() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM roles
        WHERE name = NEW.role_name
          AND (organisation_id = NEW.organisation_id OR organisation_id IS NULL)
    ) THEN
        RAISE EXCEPTION 'role % does not exist in organisation % or globally', NEW.role_name, NEW.organisation_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_check_organisation_member_role ON organisation_members;
CREATE TRIGGER trg_check_organisation_member_role
BEFORE INSERT OR UPDATE OF role_name, organisation_id ON organisation_members
FOR EACH ROW EXECUTE FUNCTION check_organisation_member_role();

CREATE OR REPLACE FUNCTION check_role_deletion() RETURNS trigger AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM organisation_members
        WHERE role_name = OLD.name
          AND (organisation_id = OLD.organisation_id OR (OLD.organisation_id IS NULL))
    ) THEN
        RAISE EXCEPTION 'cannot delete role % because it is still referenced by organisation members', OLD.name
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_check_role_deletion ON roles;
CREATE TRIGGER trg_check_role_deletion
BEFORE DELETE ON roles
FOR EACH ROW EXECUTE FUNCTION check_role_deletion();
