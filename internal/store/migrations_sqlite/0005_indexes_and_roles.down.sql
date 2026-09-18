DROP TRIGGER IF EXISTS trg_check_role_deletion;
DROP TRIGGER IF EXISTS trg_check_organisation_member_role_update;
DROP TRIGGER IF EXISTS trg_check_organisation_member_role_insert;

DROP INDEX IF EXISTS instances_heartbeat_idx;
DROP INDEX IF EXISTS audit_logs_user_idx;
DROP INDEX IF EXISTS domain_claims_org_domain_idx;
DROP INDEX IF EXISTS api_keys_org_created_idx;
DROP INDEX IF EXISTS alerting_rules_receiving_app_idx;
DROP INDEX IF EXISTS organisation_members_user_idx;
