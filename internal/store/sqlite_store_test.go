package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

func newTestSQLiteStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()

	s, err := Open(ctx, "sqlite://:memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite store: %v", err)
	}

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate sqlite store: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
	})

	return s
}

func TestSQLite_MigrationLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_migrate.db")
	rawURL := "sqlite://" + dbPath

	s, err := Open(ctx, rawURL)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate up: %v", err)
	}

	v, dirty, err := s.MigrateVersion(ctx)
	if err != nil {
		t.Fatalf("failed to get migrate version: %v", err)
	}
	if dirty {
		t.Fatalf("database is dirty after migration")
	}
	if v != 6 {
		t.Fatalf("expected migration version 6, got %d", v)
	}

	// Migrate down 1 step
	if err := s.MigrateDown(ctx, 1); err != nil {
		t.Fatalf("failed to migrate down 1 step: %v", err)
	}
	v, _, err = s.MigrateVersion(ctx)
	if err != nil {
		t.Fatalf("failed to get migrate version after down: %v", err)
	}
	if v != 5 {
		t.Fatalf("expected migration version 5, got %d", v)
	}

	// Migrate back up
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate back up: %v", err)
	}
	v, _, _ = s.MigrateVersion(ctx)
	if v != 6 {
		t.Fatalf("expected migration version 6, got %d", v)
	}
}

func TestSQLite_Transactions(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	t.Run("commit", func(t *testing.T) {
		err := s.InTx(ctx, func(q gen.Querier) error {
			_, err := q.CreateOrganisation(ctx, "tx_org_commit")
			return err
		})
		if err != nil {
			t.Fatalf("expected InTx commit to succeed, got: %v", err)
		}

		org, err := s.Queries().GetOrganisationByName(ctx, "tx_org_commit")
		if err != nil {
			t.Fatalf("expected org to exist: %v", err)
		}
		if org.Name != "tx_org_commit" {
			t.Fatalf("expected name tx_org_commit, got %s", org.Name)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		expectedErr := errors.New("simulated error")
		err := s.InTx(ctx, func(q gen.Querier) error {
			_, err := q.CreateOrganisation(ctx, "tx_org_rollback")
			if err != nil {
				return err
			}
			return expectedErr
		})
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected error %v, got %v", expectedErr, err)
		}

		_, err = s.Queries().GetOrganisationByName(ctx, "tx_org_rollback")
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected pgx.ErrNoRows for rolled back org, got: %v", err)
		}
	})
}

func TestSQLite_Truncate(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	_, err := s.Queries().CreateOrganisation(ctx, "to_truncate")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	if err := s.Truncate(ctx); err != nil {
		t.Fatalf("failed to truncate: %v", err)
	}

	_, err = s.Queries().GetOrganisationByName(ctx, "to_truncate")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows after truncate, got: %v", err)
	}

	// Verify global roles reseeded
	roles, err := s.Queries().ListRoles(ctx, pgtype.UUID{Valid: false})
	if err != nil {
		t.Fatalf("failed to list global roles: %v", err)
	}
	if len(roles) < 3 {
		t.Fatalf("expected at least 3 global roles reseeded, got %d", len(roles))
	}
}

func TestSQLite_FullCRUDSuite(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	// 1. Organisations & Applications
	org, err := s.Queries().CreateOrganisation(ctx, "test_org")
	if err != nil {
		t.Fatalf("create org failed: %v", err)
	}
	if !org.ID.Valid || org.Name != "test_org" {
		t.Fatalf("unexpected org: %+v", org)
	}

	fetchedOrg, err := s.Queries().GetOrganisationByName(ctx, "test_org")
	if err != nil || fetchedOrg.ID != org.ID {
		t.Fatalf("get org failed: %v", err)
	}

	app, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           "test_app",
		Settings:       []byte(`{"key": "val"}`),
	})
	if err != nil {
		t.Fatalf("create app failed: %v", err)
	}
	if !app.ID.Valid || app.Name != "test_app" {
		t.Fatalf("unexpected app: %+v", app)
	}

	fetchedApp, err := s.Queries().GetApplicationByName(ctx, gen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           "test_app",
	})
	if err != nil || fetchedApp.ID != app.ID {
		t.Fatalf("get app failed: %v", err)
	}

	apps, err := s.Queries().ListApplicationsByOrganisation(ctx, org.ID)
	if err != nil || len(apps) != 1 {
		t.Fatalf("list apps failed: %v", err)
	}

	updatedApp, err := s.Queries().UpdateApplicationSettings(ctx, gen.UpdateApplicationSettingsParams{
		OrganisationID: org.ID,
		Name:           "test_app",
		Settings:       []byte(`{"updated": true}`),
	})
	if err != nil || string(updatedApp.Settings) != `{"updated": true}` {
		t.Fatalf("update app settings failed: %v", err)
	}

	// 2. Instances & Executors
	instID := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true}
	inst, err := s.Queries().UpsertInstance(ctx, gen.UpsertInstanceParams{
		ID:               instID,
		AdvertiseAddress: "127.0.0.1",
		Port:             8090,
	})
	if err != nil {
		t.Fatalf("upsert instance failed: %v", err)
	}

	fetchedInst, err := s.Queries().GetInstance(ctx, instID)
	if err != nil || fetchedInst.ID != inst.ID {
		t.Fatalf("get instance failed: %v", err)
	}

	leaseExp := pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true}
	exec, err := s.Queries().UpsertExecutor(ctx, gen.UpsertExecutorParams{
		ApplicationID:      app.ID,
		ExecutorID:         "exec-1",
		ApplicationVersion: "1.0.0",
		Hostname:           "host-1",
		Metadata:           []byte(`{}`),
		OwnerInstanceID:    inst.ID,
		LeaseExpiresAt:     leaseExp,
	})
	if err != nil {
		t.Fatalf("upsert executor failed: %v", err)
	}
	if exec.ExecutorID != "exec-1" {
		t.Fatalf("unexpected executor: %+v", exec)
	}

	err = s.Queries().TouchExecutorLastSeen(ctx, gen.TouchExecutorLastSeenParams{
		ApplicationID: app.ID,
		ExecutorID:    "exec-1",
	})
	if err != nil {
		t.Fatalf("touch executor last seen failed: %v", err)
	}

	execs, err := s.Queries().ListConnectedExecutorsByApplication(ctx, app.ID)
	if err != nil || len(execs) != 1 {
		t.Fatalf("list connected executors failed: %v", err)
	}

	_, err = s.Queries().DisconnectExecutor(ctx, gen.DisconnectExecutorParams{
		ApplicationID: app.ID,
		ExecutorID:    "exec-1",
	})
	if err != nil {
		t.Fatalf("disconnect executor failed: %v", err)
	}

	execs, err = s.Queries().ListConnectedExecutorsByApplication(ctx, app.ID)
	if err != nil || len(execs) != 0 {
		t.Fatalf("expected 0 connected executors, got %d", len(execs))
	}

	// 3. API Keys
	key, err := s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             "test_key",
		Lookup:           "dbos_test_lookup",
		KeyHash:          []byte("hash123456"),
		ApplicationNames: []string{"test_app"},
		Permissions:      []string{"application.read", "application.write"},
	})
	if err != nil {
		t.Fatalf("create api key failed: %v", err)
	}
	if key.Lookup != "dbos_test_lookup" || len(key.Permissions) != 2 {
		t.Fatalf("unexpected api key: %+v", key)
	}

	fetchedKey, err := s.Queries().GetAPIKeyByLookup(ctx, "dbos_test_lookup")
	if err != nil || fetchedKey.ID != key.ID {
		t.Fatalf("get api key by lookup failed: %v", err)
	}

	revokedKey, err := s.Queries().RevokeAPIKey(ctx, gen.RevokeAPIKeyParams{
		ID:             key.ID,
		OrganisationID: org.ID,
	})
	if err != nil || !revokedKey.RevokedAt.Valid {
		t.Fatalf("revoke api key failed: %v", err)
	}

	// 4. Alerting Rules
	minInterval := int32(60)
	rule, err := s.Queries().CreateAlertingRule(ctx, gen.CreateAlertingRuleParams{
		ApplicationID:          app.ID,
		ReceivingApplicationID: app.ID,
		RuleType:               "UnresponsiveApplication",
		RuleMetadata:           []byte(`{"threshold": 1}`),
		MinIntervalSecs:        &minInterval,
	})
	if err != nil {
		t.Fatalf("create alerting rule failed: %v", err)
	}
	if rule.RuleType != "UnresponsiveApplication" {
		t.Fatalf("unexpected rule: %+v", rule)
	}

	fetchedRule, err := s.Queries().GetAlertingRule(ctx, gen.GetAlertingRuleParams{
		ID:            rule.ID,
		ApplicationID: app.ID,
	})
	if err != nil || fetchedRule.ID != rule.ID {
		t.Fatalf("get alerting rule failed: %v", err)
	}

	updatedRule, err := s.Queries().TouchAlertRuleLastFiredAtomic(ctx, gen.TouchAlertRuleLastFiredAtomicParams{
		ID:            rule.ID,
		ApplicationID: app.ID,
	})
	if err != nil || !updatedRule.LastFiredAt.Valid {
		t.Fatalf("touch alert rule atomic failed: %v", err)
	}

	deletedCount, err := s.Queries().DeleteAlertingRule(ctx, gen.DeleteAlertingRuleParams{
		ID:            rule.ID,
		ApplicationID: app.ID,
	})
	if err != nil || deletedCount != 1 {
		t.Fatalf("delete alert rule failed: count=%d, err=%v", deletedCount, err)
	}

	// 5. Users & Members & Roles
	user, err := s.Queries().CreateUser(ctx, gen.CreateUserParams{
		Username: "alice",
		Subject:  "sub:alice",
		Email:    "alice@example.com",
		IsAdmin:  false,
	})
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	fetchedUser, err := s.Queries().GetUserBySubject(ctx, "sub:alice")
	if err != nil || fetchedUser.ID != user.ID {
		t.Fatalf("get user by subject failed: %v", err)
	}

	customRole, err := s.Queries().CreateRole(ctx, gen.CreateRoleParams{
		OrganisationID: org.ID,
		Name:           "custom_auditor",
		Permissions:    []string{"audit.read"},
	})
	if err != nil || customRole.Name != "custom_auditor" {
		t.Fatalf("create custom role failed: %v", err)
	}

	member, err := s.Queries().UpsertMemberRole(ctx, gen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		RoleName:       "admin",
	})
	if err != nil || member.RoleName != "admin" {
		t.Fatalf("upsert member role failed: %v", err)
	}

	members, err := s.Queries().ListMembersByOrganisation(ctx, org.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("list members failed: %v", err)
	}
	if members[0].Username != "alice" || members[0].RoleName != "admin" {
		t.Fatalf("unexpected member row: %+v", members[0])
	}

	// 6. Domain Claims
	claim, err := s.Queries().CreateDomainClaim(ctx, gen.CreateDomainClaimParams{
		OrganisationID: org.ID,
		Domain:         "example.com",
	})
	if err != nil {
		t.Fatalf("create domain claim failed: %v", err)
	}
	if claim.Domain != "example.com" {
		t.Fatalf("unexpected claim: %+v", claim)
	}

	fetchedClaim, err := s.Queries().GetDomainClaim(ctx, "example.com")
	if err != nil || fetchedClaim.ID != claim.ID {
		t.Fatalf("get domain claim failed: %v", err)
	}

	// 7. Audit Logs
	auditLog, err := s.Queries().CreateAuditLog(ctx, gen.CreateAuditLogParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		Username:       "alice",
		Action:         "app.create",
		Details:        []byte(`{"status": "ok"}`),
	})
	if err != nil {
		t.Fatalf("create audit log failed: %v", err)
	}
	if auditLog.Action != "app.create" {
		t.Fatalf("unexpected audit log: %+v", auditLog)
	}

	logs, err := s.Queries().ListAuditLogs(ctx, gen.ListAuditLogsParams{
		OrganisationID: org.ID,
		Limit:          10,
		Offset:         0,
	})
	if err != nil || len(logs) != 1 {
		t.Fatalf("list audit logs failed: %v", err)
	}

	// 8. Recovery Dispatches
	dispatch, err := s.Queries().RecordRecoveryDispatch(ctx, gen.RecordRecoveryDispatchParams{
		ApplicationID:    app.ID,
		DeadExecutorID:   "exec-dead",
		TargetExecutorID: "exec-1",
		DispatchedAt:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
		Success:          true,
	})
	if err != nil {
		t.Fatalf("record recovery dispatch failed: %v", err)
	}
	if dispatch.DeadExecutorID != "exec-dead" {
		t.Fatalf("unexpected dispatch: %+v", dispatch)
	}

	count, err := s.Queries().CountRecentRecoveryDispatches(ctx, gen.CountRecentRecoveryDispatchesParams{
		ApplicationID:  app.ID,
		DeadExecutorID: "exec-dead",
		DispatchedAt:   pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Hour), Valid: true},
	})
	if err != nil || count != 1 {
		t.Fatalf("count recent recovery dispatches failed: count=%d, err=%v", count, err)
	}

	// 9. Delete Application
	delApp, err := s.Queries().DeleteApplication(ctx, gen.DeleteApplicationParams{
		OrganisationID: org.ID,
		Name:           "test_app",
	})
	if err != nil || delApp.ID != app.ID {
		t.Fatalf("delete application failed: %v", err)
	}
}

func TestSQLite_AuditFiltersRetentionAndExpiry(t *testing.T) {
	s := newTestSQLiteStore(t)
	ctx := context.Background()

	org, err := s.Queries().CreateOrganisation(ctx, "audit_org")
	if err != nil {
		t.Fatalf("create org failed: %v", err)
	}
	if org.AuditLogRetentionDays != 90 {
		t.Fatalf("default retention = %d, want 90", org.AuditLogRetentionDays)
	}

	seed := func(action, details string) {
		t.Helper()
		_, err := s.Queries().CreateAuditLog(ctx, gen.CreateAuditLogParams{
			OrganisationID: org.ID,
			Username:       "alice",
			Action:         action,
			Details:        []byte(details),
		})
		if err != nil {
			t.Fatalf("seed audit log: %v", err)
		}
	}
	seed("workflow.cancel", `{"status":"success","subject_type":"user","subject_id":"sub:1","subject_display":"alice@example.com","target_type":"workflow","target_id":"wf-1","application_name":"shop"}`)
	seed("workflow.cancel", `{"status":"failure","subject_type":"api_key","subject_id":"key1","subject_display":"deploy-key","target_type":"workflow","target_id":"wf-2","application_name":"shop"}`)
	seed("token.revoke", `{"status":"success","subject_type":"user","subject_id":"sub:1","subject_display":"alice","target_type":"token","target_id":"old-key"}`)

	list := func(arg gen.ListAuditLogsParams) []gen.AuditLog {
		t.Helper()
		arg.OrganisationID = org.ID
		if arg.Limit == 0 {
			arg.Limit = 100
		}
		rows, err := s.Queries().ListAuditLogs(ctx, arg)
		if err != nil {
			t.Fatalf("list audit logs: %v", err)
		}
		return rows
	}

	if got := list(gen.ListAuditLogsParams{Operation: strPtr("workflow.cancel")}); len(got) != 2 {
		t.Errorf("operation filter: got %d, want 2", len(got))
	}
	if got := list(gen.ListAuditLogsParams{Subject: strPtr("alice@example.com")}); len(got) != 1 {
		t.Errorf("subject display filter: got %d, want 1", len(got))
	}
	if got := list(gen.ListAuditLogsParams{Subject: strPtr("key1")}); len(got) != 1 {
		t.Errorf("subject id filter: got %d, want 1", len(got))
	}
	if got := list(gen.ListAuditLogsParams{Target: strPtr("wf-2")}); len(got) != 1 {
		t.Errorf("target filter: got %d, want 1", len(got))
	}
	if got := list(gen.ListAuditLogsParams{Target: strPtr("shop")}); len(got) != 0 {
		t.Errorf("target must match target_id, not application_name: got %d, want 0", len(got))
	}
	if got := list(gen.ListAuditLogsParams{}); len(got) != 3 {
		t.Errorf("expected 3 rows, got %d", len(got))
	} else {
		seen := map[string]bool{}
		for _, row := range got {
			seen[row.Action] = true
		}
		if !seen["workflow.cancel"] || !seen["token.revoke"] {
			t.Errorf("expected workflow.cancel and token.revoke rows, got %+v", got)
		}
	}
	if got := list(gen.ListAuditLogsParams{Limit: 2}); len(got) != 2 {
		t.Errorf("limit 2: got %d, want 2", len(got))
	}
	if got := list(gen.ListAuditLogsParams{Limit: 10, Offset: 2}); len(got) != 1 {
		t.Errorf("offset 2: got %d, want 1", len(got))
	}
	future := pgtype.Timestamptz{Time: time.Now().UTC().Add(time.Hour), Valid: true}
	if got := list(gen.ListAuditLogsParams{StartTime: future}); len(got) != 0 {
		t.Errorf("future startTime: got %d, want 0", len(got))
	}
	past := pgtype.Timestamptz{Time: time.Now().UTC().Add(-time.Hour), Valid: true}
	if got := list(gen.ListAuditLogsParams{EndTime: past}); len(got) != 0 {
		t.Errorf("past endTime: got %d, want 0", len(got))
	}

	updated, err := s.Queries().UpdateOrganisation(ctx, gen.UpdateOrganisationParams{
		ID:                    org.ID,
		AuditLogRetentionDays: int32Ptr(7),
	})
	if err != nil {
		t.Fatalf("update retention: %v", err)
	}
	if updated.AuditLogRetentionDays != 7 {
		t.Fatalf("retention = %d, want 7", updated.AuditLogRetentionDays)
	}

	deleted, err := s.Queries().DeleteExpiredAuditLogs(ctx, gen.DeleteExpiredAuditLogsParams{
		OrganisationID: org.ID,
		Cutoff:         pgtype.Timestamptz{Time: time.Now().UTC().Add(time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if deleted != 3 {
		t.Errorf("deleted = %d, want 3", deleted)
	}
	if got := list(gen.ListAuditLogsParams{}); len(got) != 0 {
		t.Errorf("expected empty log after expiry purge, got %d", len(got))
	}
}

func strPtr(s string) *string { return &s }

func int32Ptr(v int32) *int32 { return &v }
