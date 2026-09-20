package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/protocol"
	storegen "github.com/abn/relay/internal/store/gen"
)

func auditTestOrgID() pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{9, 9, 9}, Valid: true}
}

func decodeAuditDetails(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode audit details: %v", err)
	}
	return m
}

func TestAuditOperationEnvelopeAPIKey(t *testing.T) {
	var captured storegen.CreateAuditLogParams
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			captured = arg
			return storegen.AuditLog{}, nil
		},
		listAPIKeysFunc: func(ctx context.Context, orgID pgtype.UUID) ([]storegen.ApiKey, error) {
			return []storegen.ApiKey{{Name: "old-key"}}, nil
		},
		revokeAPIKeyFunc: func(ctx context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error) {
			return storegen.ApiKey{}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	keyCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{
		Subject:     "keylookup123",
		Username:    "deploy-key",
		IsAPIKey:    true,
		Permissions: []string{auth.PermApplicationRead, auth.PermApplicationWrite},
		Role:        auth.RoleAdmin,
	})

	_, err := srv.DeleteToken(keyCtx, gen.DeleteTokenRequestObject{OrgName: "acme", TokenName: "old-key"})
	if err != nil {
		t.Fatalf("DeleteToken error: %v", err)
	}
	if captured.Action != "token.revoke" {
		t.Fatalf("action = %q, want token.revoke", captured.Action)
	}
	if captured.Username != "deploy-key" {
		t.Errorf("username = %q, want deploy-key", captured.Username)
	}
	details := decodeAuditDetails(t, captured.Details)
	for k, want := range map[string]string{
		"status":          "success",
		"subject_type":    "api_key",
		"subject_id":      "keylookup123",
		"subject_display": "deploy-key",
		"target_type":     "token",
		"target_id":       "old-key",
	} {
		if details[k] != want {
			t.Errorf("details[%q] = %v, want %q", k, details[k], want)
		}
	}
}

func TestAuditOperationEnvelopeUser(t *testing.T) {
	var captured storegen.CreateAuditLogParams
	calls := 0
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			calls++
			captured = arg
			return storegen.AuditLog{}, nil
		},
		getUserByUsernameFunc: func(ctx context.Context, username string) (storegen.User, error) {
			return storegen.User{Username: username}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	userCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{
		Subject:  "user-sub-1",
		Username: "alice",
		Email:    "alice@example.com",
		IsAdmin:  true,
		Role:     auth.RoleAdmin,
	})

	_, err := srv.RemoveMember(userCtx, gen.RemoveMemberRequestObject{OrgName: "acme", Username: "bob"})
	if err != nil {
		t.Fatalf("RemoveMember error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 audit entry, got %d", calls)
	}
	if captured.Action != "user.remove" {
		t.Fatalf("action = %q, want user.remove", captured.Action)
	}
	details := decodeAuditDetails(t, captured.Details)
	if details["subject_type"] != "user" || details["subject_id"] != "user-sub-1" || details["subject_display"] != "alice@example.com" {
		t.Errorf("unexpected subject envelope: %v", details)
	}
	if details["target_type"] != "user" || details["target_id"] != "bob" {
		t.Errorf("unexpected target envelope: %v", details)
	}
}

func TestAuditFailureEntries(t *testing.T) {
	var actions []string
	var statuses []string
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		listAPIKeysFunc: func(ctx context.Context, orgID pgtype.UUID) ([]storegen.ApiKey, error) {
			return nil, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			actions = append(actions, arg.Action)
			var m map[string]any
			_ = json.Unmarshal(arg.Details, &m)
			statuses = append(statuses, m["status"].(string))
			return storegen.AuditLog{}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	viewerCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{
		Subject:  "user-sub-2",
		Username: "mallory",
		Role:     auth.RoleViewer,
	})

	// Forbidden role creation is recorded as a failure.
	resp, err := srv.CreateRole(viewerCtx, gen.CreateRoleRequestObject{
		OrgName: "acme",
		Body:    &gen.CreateRoleJSONRequestBody{Name: "auditor"},
	})
	if err != nil {
		t.Fatalf("CreateRole error: %v", err)
	}
	if r, ok := resp.(gen.CreateRoledefaultApplicationProblemPlusJSONResponse); !ok || r.StatusCode != 403 {
		t.Fatalf("expected 403, got %+v", resp)
	}

	// Unknown token revocation is recorded as a failure.
	adminCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{
		Subject:  "admin-sub",
		Username: "admin",
		IsAdmin:  true,
		Role:     auth.RoleAdmin,
	})
	delResp, err := srv.DeleteToken(adminCtx, gen.DeleteTokenRequestObject{OrgName: "acme", TokenName: "missing"})
	if err != nil {
		t.Fatalf("DeleteToken error: %v", err)
	}
	if r, ok := delResp.(gen.DeleteTokendefaultApplicationProblemPlusJSONResponse); !ok || r.StatusCode != 404 {
		t.Fatalf("expected 404, got %+v", delResp)
	}

	if len(actions) != 2 || actions[0] != "role.create" || actions[1] != "token.revoke" {
		t.Fatalf("unexpected audit actions: %v", actions)
	}
	for i, s := range statuses {
		if s != "failure" {
			t.Errorf("actions[%d] status = %q, want failure", i, s)
		}
	}
}

func TestAuditGenerateSecret(t *testing.T) {
	var entries []storegen.CreateAuditLogParams
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			entries = append(entries, arg)
			return storegen.AuditLog{}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	adminCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{
		Subject: "admin-sub", Username: "admin", IsAdmin: true, Role: auth.RoleAdmin,
	})
	viewerCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{
		Subject: "viewer-sub", Username: "mallory", Role: auth.RoleViewer,
		Permissions: []string{auth.PermApplicationRead},
	})

	resp, err := srv.GenerateSecret(adminCtx, gen.GenerateSecretRequestObject{OrgName: "acme"})
	if err != nil {
		t.Fatalf("GenerateSecret error: %v", err)
	}
	created, ok := resp.(gen.GenerateSecret201JSONResponse)
	if !ok || created.Secret == "" {
		t.Fatalf("expected 201 with secret, got %+v", resp)
	}
	resp, err = srv.GenerateSecret(viewerCtx, gen.GenerateSecretRequestObject{OrgName: "acme"})
	if err != nil {
		t.Fatalf("GenerateSecret error: %v", err)
	}
	if rej, ok := resp.(gen.GenerateSecretdefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != 403 {
		t.Fatalf("expected 403, got %+v", resp)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	for i, want := range []string{"success", "failure"} {
		details := decodeAuditDetails(t, entries[i].Details)
		if entries[i].Action != "secret.generate" || details["status"] != want {
			t.Errorf("entries[%d] = %q %v, want secret.generate %s", i, entries[i].Action, details["status"], want)
		}
		if _, leaked := details["secret"]; leaked {
			t.Errorf("entries[%d] must not contain the secret value", i)
		}
	}
}

func TestAuditListingMapping(t *testing.T) {
	structured := storegen.AuditLog{
		ID:             makeUUID(70),
		OrganisationID: auditTestOrgID(),
		Username:       "deploy-key",
		Action:         "token.revoke",
		Details: []byte(`{"status":"success","subject_type":"api_key","subject_id":"keylookup123",` +
			`"subject_display":"deploy-key","target_type":"token","target_id":"old-key","source_ip":"203.0.113.7"}`),
		CreatedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	legacy := storegen.AuditLog{
		ID:             makeUUID(71),
		OrganisationID: auditTestOrgID(),
		Username:       "alice",
		Action:         "role.create",
		Details:        []byte(`{"permissions":["application.read"]}`),
		CreatedAt:      pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		listAuditLogsFunc: func(ctx context.Context, arg storegen.ListAuditLogsParams) ([]storegen.AuditLog, error) {
			return []storegen.AuditLog{structured, legacy}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	adminCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{Subject: "x", IsAdmin: true})
	resp, err := srv.ListAuditLogs(adminCtx, gen.ListAuditLogsRequestObject{OrgName: "acme"})
	if err != nil {
		t.Fatalf("ListAuditLogs error: %v", err)
	}
	entries, ok := resp.(gen.ListAuditLogs200JSONResponse)
	if !ok || len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %+v", resp)
	}

	first := entries[0]
	if first.Subject.Type != gen.AuditSubjectTypeApiKey || first.Subject.Id != "keylookup123" || first.Subject.Display != "deploy-key" {
		t.Errorf("unexpected subject: %+v", first.Subject)
	}
	if first.Target == nil || first.Target.Type != gen.AuditTargetTypeToken || first.Target.Id != "old-key" {
		t.Errorf("unexpected target: %+v", first.Target)
	}
	if first.Status != gen.Success || first.SourceIp != "203.0.113.7" || first.Operation != "token.revoke" {
		t.Errorf("unexpected entry meta: %+v", first)
	}

	second := entries[1]
	if second.Subject.Type != gen.AuditSubjectTypeUser || second.Subject.Display != "alice" {
		t.Errorf("legacy row must default to user subject, got %+v", second.Subject)
	}
	if second.Target != nil {
		t.Errorf("legacy row without target envelope must omit target, got %+v", second.Target)
	}
}

func TestAuditListingFilters(t *testing.T) {
	var captured storegen.ListAuditLogsParams
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		listAuditLogsFunc: func(ctx context.Context, arg storegen.ListAuditLogsParams) ([]storegen.AuditLog, error) {
			captured = arg
			return []storegen.AuditLog{}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	adminCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{Subject: "x", IsAdmin: true})
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC()
	op, subj, tgt := "workflow.cancel", "alice@example.com", "wf-1"
	limit, offset := int64(10), int64(5)
	_, err := srv.ListAuditLogs(adminCtx, gen.ListAuditLogsRequestObject{
		OrgName: "acme",
		Params: gen.ListAuditLogsParams{
			StartTime: &start, EndTime: &end,
			Operation: &op, Subject: &subj, Target: &tgt,
			Limit: &limit, Offset: &offset,
		},
	})
	if err != nil {
		t.Fatalf("ListAuditLogs error: %v", err)
	}
	if captured.Operation == nil || *captured.Operation != op {
		t.Errorf("operation filter not passed through: %+v", captured.Operation)
	}
	if captured.Subject == nil || *captured.Subject != subj {
		t.Errorf("subject filter not passed through: %+v", captured.Subject)
	}
	if captured.Target == nil || *captured.Target != tgt {
		t.Errorf("target filter not passed through: %+v", captured.Target)
	}
	if captured.Limit != 10 || captured.Offset != 5 {
		t.Errorf("paging not passed through: %+v", captured)
	}
	if !captured.StartTime.Valid || !captured.EndTime.Valid {
		t.Errorf("time window not passed through: %+v", captured)
	}
}

func TestUpdateOrgRetention(t *testing.T) {
	org := storegen.Organisation{ID: auditTestOrgID(), Name: "acme", AuditLogRetentionDays: 90}
	var updatedRetention int32 = -1
	var updatedName string
	var auditedOp string
	var auditedDetails map[string]any
	auditCalls := 0
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			if name != "acme" {
				return storegen.Organisation{}, errors.New("not found")
			}
			return org, nil
		},
		updateOrgFunc: func(ctx context.Context, arg storegen.UpdateOrganisationParams) (storegen.Organisation, error) {
			if arg.AuditLogRetentionDays != nil {
				updatedRetention = *arg.AuditLogRetentionDays
				org.AuditLogRetentionDays = *arg.AuditLogRetentionDays
			}
			if arg.Name != nil {
				updatedName = *arg.Name
				org.Name = *arg.Name
			}
			return org, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			auditCalls++
			auditedOp = arg.Action
			_ = json.Unmarshal(arg.Details, &auditedDetails)
			return storegen.AuditLog{}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	adminCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{Subject: "a", Username: "admin", IsAdmin: true})

	// A PATCH changing nothing returns 204 without recording an entry.
	noChangeResp, err := srv.UpdateOrg(adminCtx, gen.UpdateOrgRequestObject{OrgName: "acme"})
	if err != nil {
		t.Fatalf("UpdateOrg error: %v", err)
	}
	if _, ok := noChangeResp.(gen.UpdateOrg204Response); !ok {
		t.Fatalf("expected 204, got %+v", noChangeResp)
	}
	if auditCalls != 0 {
		t.Fatalf("no-change PATCH must not record entries, got %d", auditCalls)
	}

	for _, days := range []int32{6, 3651} {
		resp, err := srv.UpdateOrg(adminCtx, gen.UpdateOrgRequestObject{
			OrgName: "acme",
			Body:    &gen.UpdateOrgJSONRequestBody{AuditLogRetentionDays: &days},
		})
		if err != nil {
			t.Fatalf("UpdateOrg error: %v", err)
		}
		if r, ok := resp.(gen.UpdateOrgdefaultApplicationProblemPlusJSONResponse); !ok || r.StatusCode != 422 {
			t.Errorf("days=%d: expected 422, got %+v", days, resp)
		}
	}

	days := int32(30)
	resp, err := srv.UpdateOrg(adminCtx, gen.UpdateOrgRequestObject{
		OrgName: "acme",
		Body:    &gen.UpdateOrgJSONRequestBody{AuditLogRetentionDays: &days},
	})
	if err != nil {
		t.Fatalf("UpdateOrg error: %v", err)
	}
	if _, ok := resp.(gen.UpdateOrg204Response); !ok {
		t.Fatalf("expected 204, got %+v", resp)
	}
	if updatedRetention != 30 {
		t.Errorf("retention not persisted, got %d", updatedRetention)
	}
	if auditedOp != "organization.update" {
		t.Errorf("audit op = %q, want organization.update", auditedOp)
	}
	if auditedDetails["audit_log_retention_days"] != float64(30) {
		t.Errorf("audit details missing retention: %v", auditedDetails)
	}

	// Rename conflict is rejected.
	taken := "taken"
	store.getOrgByNameFunc = func(ctx context.Context, name string) (storegen.Organisation, error) {
		return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
	}
	resp, err = srv.UpdateOrg(adminCtx, gen.UpdateOrgRequestObject{
		OrgName: "acme",
		Body:    &gen.UpdateOrgJSONRequestBody{NewName: &taken},
	})
	if err != nil {
		t.Fatalf("UpdateOrg error: %v", err)
	}
	if r, ok := resp.(gen.UpdateOrgdefaultApplicationProblemPlusJSONResponse); !ok || r.StatusCode != 409 {
		t.Errorf("expected 409 for taken name, got %+v", resp)
	}
	_ = updatedName
}

func TestGetOrgRetentionDays(t *testing.T) {
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name, AuditLogRetentionDays: 30}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	adminCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{Subject: "a", IsAdmin: true})
	resp, err := srv.GetOrg(adminCtx, gen.GetOrgRequestObject{OrgName: "acme"})
	if err != nil {
		t.Fatalf("GetOrg error: %v", err)
	}
	got, ok := resp.(gen.GetOrg200JSONResponse)
	if !ok || got.AuditLogRetentionDays != 30 {
		t.Errorf("expected retention 30, got %+v", resp)
	}
}

type stubRetentionStore struct {
	orgs    []storegen.Organisation
	cutoffs map[string]time.Time
	deleted int64
}

func (s *stubRetentionStore) ListAllOrganisations(_ context.Context) ([]storegen.Organisation, error) {
	return s.orgs, nil
}

func (s *stubRetentionStore) DeleteExpiredAuditLogs(_ context.Context, arg storegen.DeleteExpiredAuditLogsParams) (int64, error) {
	s.cutoffs[arg.OrganisationID.String()] = arg.Cutoff.Time
	return s.deleted, nil
}

func TestAuditRetentionSweep(t *testing.T) {
	orgA := storegen.Organisation{ID: makeUUID(80), Name: "a", AuditLogRetentionDays: 7}
	orgB := storegen.Organisation{ID: makeUUID(81), Name: "b", AuditLogRetentionDays: 90}
	orgC := storegen.Organisation{ID: makeUUID(82), Name: "c", AuditLogRetentionDays: 0}
	stub := &stubRetentionStore{orgs: []storegen.Organisation{orgA, orgB, orgC}, cutoffs: map[string]time.Time{}, deleted: 3}
	sweeper := api.NewAuditRetentionSweeper(stub, nil)
	total, err := sweeper.SweepOnce(context.Background())
	if err != nil {
		t.Fatalf("SweepOnce error: %v", err)
	}
	if total != 9 {
		t.Errorf("total = %d, want 9", total)
	}
	now := time.Now().UTC()
	for org, days := range map[storegen.Organisation]int{orgA: 7, orgB: 90, orgC: 90} {
		cutoff, ok := stub.cutoffs[org.ID.String()]
		if !ok {
			t.Fatalf("no cutoff for org %s", org.Name)
		}
		want := now.Add(-time.Duration(days) * 24 * time.Hour)
		if cutoff.After(want.Add(time.Minute)) || cutoff.Before(want.Add(-time.Minute)) {
			t.Errorf("org %s cutoff = %v, want ~%v", org.Name, cutoff, want)
		}
	}
}

func TestMiddlewareDenialRecording(t *testing.T) {
	orgID := auditTestOrgID()
	var captured storegen.CreateAuditLogParams
	calls := 0
	plain, rec, err := auth.Mint()
	if err != nil {
		t.Fatalf("mint key: %v", err)
	}
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: orgID, Name: name}, nil
		},
		getAPIKeyByLookupFunc: func(ctx context.Context, lookup string) (storegen.ApiKey, error) {
			return storegen.ApiKey{
				Lookup:           rec.Lookup,
				KeyHash:          rec.Hash,
				OrganisationID:   orgID,
				Name:             "weak-key",
				Permissions:      []string{auth.PermWebsocketConnect},
				ApplicationNames: []string{},
			}, nil
		},
		touchAPIKeyLastUsedFunc: func(ctx context.Context, id pgtype.UUID) error { return nil },
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			calls++
			captured = arg
			return storegen.AuditLog{}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	mux := http.NewServeMux()
	mux.Handle("POST /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/cancel",
		api.AuthMiddleware(srv)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})))

	req := httptest.NewRequest("POST", "/v2/orgs/acme/apps/shop/workflows/abc/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if calls != 1 {
		t.Fatalf("expected 1 denial audit entry, got %d", calls)
	}
	if captured.Action != "workflow.cancel" {
		t.Fatalf("action = %q, want workflow.cancel", captured.Action)
	}
	details := decodeAuditDetails(t, captured.Details)
	for k, want := range map[string]string{
		"status": "failure", "subject_type": "api_key", "subject_display": "weak-key",
		"target_type": "workflow", "target_id": "abc", "application_name": "shop",
	} {
		if details[k] != want {
			t.Errorf("details[%q] = %v, want %q", k, details[k], want)
		}
	}
}

func TestMiddlewareDenialSkipsReads(t *testing.T) {
	calls := 0
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			calls++
			return storegen.AuditLog{}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	mux := http.NewServeMux()
	mux.Handle("GET /v2/orgs/{orgName}/apps/{appName}/workflows",
		api.AuthMiddleware(srv)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))

	req := httptest.NewRequest("GET", "/v2/orgs/acme/apps/shop/workflows", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if calls != 0 {
		t.Errorf("unauthenticated reads must not record entries, got %d", calls)
	}
}

func TestDomainClaimFailures(t *testing.T) {
	var actions []string
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			actions = append(actions, arg.Action)
			return storegen.AuditLog{}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	viewerCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{Subject: "v", Username: "mallory", Role: auth.RoleViewer})

	if _, err := srv.RequestDomainClaim(viewerCtx, gen.RequestDomainClaimRequestObject{
		OrgName: "acme", Body: &gen.RequestDomainClaimJSONRequestBody{Domain: "example.com"},
	}); err != nil {
		t.Fatalf("RequestDomainClaim error: %v", err)
	}
	if _, err := srv.ReleaseDomainClaim(viewerCtx, gen.ReleaseDomainClaimRequestObject{OrgName: "acme", Domain: "example.com"}); err != nil {
		t.Fatalf("ReleaseDomainClaim error: %v", err)
	}
	if len(actions) != 2 || actions[0] != "domain_claim.create" || actions[1] != "domain_claim.delete" {
		t.Fatalf("unexpected failure actions: %v", actions)
	}
}

func TestBulkIDsNeverNull(t *testing.T) {
	var details map[string]any
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			_ = json.Unmarshal(arg.Details, &details)
			return storegen.AuditLog{}, nil
		},
	}
	srv := api.NewServer(nil, store, nil).WithAuth(true, nil)
	adminCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{Subject: "a", IsAdmin: true})
	if _, err := srv.BulkCancelWorkflows(adminCtx, gen.BulkCancelWorkflowsRequestObject{OrgName: "acme", AppName: "shop"}); err != nil {
		t.Fatalf("BulkCancelWorkflows error: %v", err)
	}
	ids, ok := details["workflow_ids"].([]any)
	if !ok || ids == nil {
		t.Fatalf("workflow_ids must be an array, got %v", details["workflow_ids"])
	}
	if len(ids) != 0 {
		t.Errorf("workflow_ids = %v, want []", ids)
	}
}

func TestForkTraceability(t *testing.T) {
	var entries []storegen.CreateAuditLogParams
	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: auditTestOrgID(), Name: name}, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			entries = append(entries, arg)
			return storegen.AuditLog{}, nil
		},
	}
	r := &mockWorkflowRouter{
		dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
			switch msg.(type) {
			case *protocol.ForkWorkflowRequest:
				return &protocol.ForkWorkflowResponse{
					Envelope:      protocol.Envelope{Type: protocol.MessageTypeForkWorkflow, RequestID: msg.GetRequestID()},
					NewWorkflowID: auditStrPtr("forked-1"),
				}, nil
			case *protocol.ForkFromFailureRequest:
				return &protocol.ForkFromFailureResponse{
					Envelope:          protocol.Envelope{Type: protocol.MessageTypeForkFromFailure, RequestID: msg.GetRequestID()},
					ForkedWorkflowIDs: []string{"forked-9"},
				}, nil
			}
			return nil, nil
		},
	}
	srv := api.NewServer(r, store, nil).WithAuth(true, nil)
	adminCtx := auth.WithIdentity(context.Background(), &auth.UserIdentity{Subject: "a", IsAdmin: true})

	if _, err := srv.ForkWorkflow(adminCtx, gen.ForkWorkflowRequestObject{
		OrgName: "acme", AppName: "shop", WorkflowId: "wf-1",
		Body: &gen.ForkWorkflowJSONRequestBody{StartStep: ptr(int32(2))},
	}); err != nil {
		t.Fatalf("ForkWorkflow error: %v", err)
	}
	if _, err := srv.BulkForkWorkflowsFromFailure(adminCtx, gen.BulkForkWorkflowsFromFailureRequestObject{
		OrgName: "acme", AppName: "shop",
		Body: &gen.BulkForkWorkflowsFromFailureJSONRequestBody{WorkflowIds: []string{"wf-2"}},
	}); err != nil {
		t.Fatalf("BulkFork error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	var forkDetails, bulkDetails map[string]any
	_ = json.Unmarshal(entries[0].Details, &forkDetails)
	_ = json.Unmarshal(entries[1].Details, &bulkDetails)
	if entries[0].Action != "workflow.fork" || forkDetails["new_workflow_id"] != "forked-1" {
		t.Errorf("fork entry = %q %v", entries[0].Action, forkDetails)
	}
	if forkDetails["target_id"] != "wf-1" {
		t.Errorf("fork target = %v, want source workflow wf-1", forkDetails["target_id"])
	}
	if entries[1].Action != "workflow.fork_from_failure" {
		t.Errorf("bulk fork action = %q", entries[1].Action)
	}
	if got, _ := bulkDetails["forked_workflow_ids"].([]any); len(got) != 1 || got[0] != "forked-9" {
		t.Errorf("forked_workflow_ids = %v", bulkDetails["forked_workflow_ids"])
	}
}

func auditStrPtr(s string) *string { return &s }
