package conformance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/problem"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	storegen "github.com/abn/relay/internal/store/gen"
)

type inMemoryStore struct {
	orgs      map[string]storegen.Organisation
	apps      map[string]storegen.Application
	executors map[string][]storegen.Executor
	keys      map[string]storegen.ApiKey
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{
		orgs:      make(map[string]storegen.Organisation),
		apps:      make(map[string]storegen.Application),
		executors: make(map[string][]storegen.Executor),
		keys:      make(map[string]storegen.ApiKey),
	}
}

func (m *inMemoryStore) Ping(_ context.Context) error {
	return nil
}

func (m *inMemoryStore) GetOrganisationByName(_ context.Context, name string) (storegen.Organisation, error) {
	if org, ok := m.orgs[name]; ok {
		return org, nil
	}
	return storegen.Organisation{}, fmt.Errorf("org not found: %s", name)
}

func (m *inMemoryStore) UpsertOrganisation(_ context.Context, name string) (storegen.Organisation, error) {
	if org, ok := m.orgs[name]; ok {
		return org, nil
	}
	org := storegen.Organisation{
		ID:   pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true},
		Name: name,
	}
	m.orgs[name] = org
	return org, nil
}

func (m *inMemoryStore) GetApplicationByName(_ context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
	if app, ok := m.apps[arg.Name]; ok {
		return app, nil
	}
	return storegen.Application{}, fmt.Errorf("app not found: %s", arg.Name)
}

func (m *inMemoryStore) ListApplicationsByOrganisation(_ context.Context, _ pgtype.UUID) ([]storegen.Application, error) {
	res := make([]storegen.Application, 0, len(m.apps))
	for _, app := range m.apps {
		res = append(res, app)
	}
	return res, nil
}

func (m *inMemoryStore) UpsertApplication(_ context.Context, arg storegen.UpsertApplicationParams) (storegen.Application, error) {
	app := storegen.Application{
		ID:             pgtype.UUID{Bytes: [16]byte{4, 5, 6}, Valid: true},
		OrganisationID: arg.OrganisationID,
		Name:           arg.Name,
		Settings:       arg.Settings,
	}
	m.apps[arg.Name] = app
	return app, nil
}

func (m *inMemoryStore) UpdateApplicationSettings(_ context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error) {
	app, ok := m.apps[arg.Name]
	if !ok {
		return storegen.Application{}, fmt.Errorf("app not found: %s", arg.Name)
	}
	app.Settings = arg.Settings
	m.apps[arg.Name] = app
	return app, nil
}

func (m *inMemoryStore) DeleteApplication(_ context.Context, arg storegen.DeleteApplicationParams) (storegen.Application, error) {
	app, ok := m.apps[arg.Name]
	if !ok {
		return storegen.Application{}, fmt.Errorf("app not found: %s", arg.Name)
	}
	delete(m.apps, arg.Name)
	return app, nil
}

func (m *inMemoryStore) ListExecutorsByApplication(_ context.Context, appID pgtype.UUID) ([]storegen.Executor, error) {
	return m.executors[formatUUID(appID)], nil
}

func (m *inMemoryStore) ListAPIKeys(_ context.Context, _ pgtype.UUID) ([]storegen.ApiKey, error) {
	res := make([]storegen.ApiKey, 0, len(m.keys))
	for _, k := range m.keys {
		if k.RevokedAt.Time.IsZero() {
			res = append(res, k)
		}
	}
	return res, nil
}

func (m *inMemoryStore) CreateAPIKey(_ context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error) {
	key := storegen.ApiKey{
		ID:               pgtype.UUID{Bytes: [16]byte{7, 8, 9}, Valid: true},
		OrganisationID:   arg.OrganisationID,
		Name:             arg.Name,
		Lookup:           arg.Lookup,
		KeyHash:          arg.KeyHash,
		ApplicationNames: arg.ApplicationNames,
		Permissions:      arg.Permissions,
	}
	m.keys[arg.Name] = key
	return key, nil
}

func (m *inMemoryStore) GetAPIKeyByLookup(_ context.Context, lookup string) (storegen.ApiKey, error) {
	for _, k := range m.keys {
		if k.Lookup == lookup && k.RevokedAt.Time.IsZero() {
			return k, nil
		}
	}
	return storegen.ApiKey{}, fmt.Errorf("token not found")
}

func (m *inMemoryStore) RevokeAPIKey(_ context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error) {
	for name, k := range m.keys {
		if k.Name == arg.ID.String() || k.ID == arg.ID {
			k.RevokedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			m.keys[name] = k
			return k, nil
		}
	}
	return storegen.ApiKey{}, fmt.Errorf("token not found")
}

func (m *inMemoryStore) CreateAlertingRule(_ context.Context, arg storegen.CreateAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{
		ID:                     pgtype.UUID{Bytes: [16]byte{1, 1, 1}, Valid: true},
		ApplicationID:          arg.ApplicationID,
		ReceivingApplicationID: arg.ReceivingApplicationID,
		RuleType:               arg.RuleType,
		RuleMetadata:           arg.RuleMetadata,
		MinIntervalSecs:        arg.MinIntervalSecs,
		CreatedAt:              pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}, nil
}

func (m *inMemoryStore) GetAlertingRule(_ context.Context, _ storegen.GetAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{}, fmt.Errorf("rule not found")
}

func (m *inMemoryStore) ListAlertingRulesByApplication(_ context.Context, _ pgtype.UUID) ([]storegen.AlertingRule, error) {
	return nil, nil
}

func (m *inMemoryStore) DeleteAlertingRule(_ context.Context, _ storegen.DeleteAlertingRuleParams) (int64, error) {
	return 1, nil
}

func (m *inMemoryStore) CreateUser(ctx context.Context, arg storegen.CreateUserParams) (storegen.User, error) { return storegen.User{}, nil }
func (m *inMemoryStore) UpsertUser(ctx context.Context, arg storegen.UpsertUserParams) (storegen.User, error) { return storegen.User{}, nil }
func (m *inMemoryStore) GetUserBySubject(ctx context.Context, subject string) (storegen.User, error) { return storegen.User{}, nil }
func (m *inMemoryStore) GetUserByUsername(ctx context.Context, username string) (storegen.User, error) { return storegen.User{}, nil }
func (m *inMemoryStore) GetUserByID(ctx context.Context, id pgtype.UUID) (storegen.User, error) { return storegen.User{}, nil }
func (m *inMemoryStore) ListMembersByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]storegen.ListMembersByOrganisationRow, error) { return nil, nil }
func (m *inMemoryStore) GetMember(ctx context.Context, arg storegen.GetMemberParams) (storegen.GetMemberRow, error) { return storegen.GetMemberRow{}, nil }
func (m *inMemoryStore) UpsertMemberRole(ctx context.Context, arg storegen.UpsertMemberRoleParams) (storegen.OrganisationMember, error) { return storegen.OrganisationMember{}, nil }
func (m *inMemoryStore) RemoveMember(ctx context.Context, arg storegen.RemoveMemberParams) (storegen.OrganisationMember, error) { return storegen.OrganisationMember{}, nil }
func (m *inMemoryStore) GetUserPrimaryOrganisation(ctx context.Context, userID pgtype.UUID) (storegen.GetUserPrimaryOrganisationRow, error) { return storegen.GetUserPrimaryOrganisationRow{}, nil }
func (m *inMemoryStore) ListRoles(ctx context.Context, organisationID pgtype.UUID) ([]storegen.Role, error) { return nil, nil }
func (m *inMemoryStore) GetRole(ctx context.Context, arg storegen.GetRoleParams) (storegen.Role, error) { return storegen.Role{}, nil }
func (m *inMemoryStore) CreateRole(ctx context.Context, arg storegen.CreateRoleParams) (storegen.Role, error) { return storegen.Role{}, nil }
func (m *inMemoryStore) DeleteRole(ctx context.Context, arg storegen.DeleteRoleParams) (storegen.Role, error) { return storegen.Role{}, nil }
func (m *inMemoryStore) ListDomainClaims(ctx context.Context, organisationID pgtype.UUID) ([]storegen.DomainClaim, error) { return nil, nil }
func (m *inMemoryStore) GetDomainClaim(ctx context.Context, domain string) (storegen.DomainClaim, error) { return storegen.DomainClaim{}, nil }
func (m *inMemoryStore) CreateDomainClaim(ctx context.Context, arg storegen.CreateDomainClaimParams) (storegen.DomainClaim, error) { return storegen.DomainClaim{}, nil }
func (m *inMemoryStore) DeleteDomainClaim(ctx context.Context, arg storegen.DeleteDomainClaimParams) (storegen.DomainClaim, error) { return storegen.DomainClaim{}, nil }
func (m *inMemoryStore) CreateAuditLog(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) { return storegen.AuditLog{}, nil }
func (m *inMemoryStore) ListAuditLogs(ctx context.Context, arg storegen.ListAuditLogsParams) ([]storegen.AuditLog, error) { return nil, nil }

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type mockRouter struct {
	handler func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error)
}

func (m *mockRouter) Dispatch(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
	if m.handler != nil {
		return m.handler(ctx, orgName, appName, msg)
	}
	return nil, nil
}

func setupTestServer(t *testing.T, routerFn func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error)) (*httptest.Server, *inMemoryStore) {
	t.Helper()
	store := newInMemoryStore()
	_, _ = store.UpsertOrganisation(context.Background(), "local")

	r := &mockRouter{handler: routerFn}
	server := api.NewServer(r, store, slog.Default())
	handler := api.NewHandler(store, server)

	ts := httptest.NewServer(handler)
	return ts, store
}

func TestConformance_OIDCStubsReturn404Problem(t *testing.T) {
	ts, _ := setupTestServer(t, nil)
	defer ts.Close()

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v2/users/me"},
		{http.MethodPost, "/v2/users"},
		{http.MethodGet, "/v2/orgs/local"},
		{http.MethodPatch, "/v2/orgs/local"},
		{http.MethodPost, "/v2/orgs/local/join"},
		{http.MethodPost, "/v2/orgs/local/secrets"},
		{http.MethodGet, "/v2/orgs/local/members"},
		{http.MethodDelete, "/v2/orgs/local/members/testuser"},
		{http.MethodPut, "/v2/orgs/local/members/testuser/roles/admin"},
		{http.MethodGet, "/v2/orgs/local/roles"},
		{http.MethodPost, "/v2/orgs/local/roles"},
		{http.MethodDelete, "/v2/orgs/local/roles/admin"},
		{http.MethodGet, "/v2/orgs/local/domain-claims"},
		{http.MethodPost, "/v2/orgs/local/domain-claims"},
		{http.MethodDelete, "/v2/orgs/local/domain-claims/example.com"},
		{http.MethodGet, "/v2/orgs/local/audit-logs"},
	}

	for _, tc := range paths {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}

			var prob problem.Problem
			if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
				t.Fatalf("decoding problem json: %v", err)
			}
			if prob.Status != http.StatusNotFound {
				t.Errorf("problem status = %d, want 404", prob.Status)
			}
		})
	}
}

func TestConformance_AppLifecycle(t *testing.T) {
	ts, _ := setupTestServer(t, nil)
	defer ts.Close()

	// 1. Register application
	registerBody := `{"privateMode":false}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/v2/orgs/local/apps/test-app", strings.NewReader(registerBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("register app status = %d, want 204, 200, or 201", resp.StatusCode)
	}

	// 2. Get application
	resp, err = http.Get(ts.URL + "/v2/orgs/local/apps/test-app")
	if err != nil {
		t.Fatalf("get app: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get app status = %d, want 200", resp.StatusCode)
	}
	var app gen.Application
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		t.Fatalf("decode app: %v", err)
	}
	if app.Name != "test-app" {
		t.Errorf("app name = %q, want test-app", app.Name)
	}

	// 3. List applications
	resp, err = http.Get(ts.URL + "/v2/orgs/local/apps")
	if err != nil {
		t.Fatalf("list apps: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list apps status = %d, want 200", resp.StatusCode)
	}
	var apps []gen.Application
	if err := json.NewDecoder(resp.Body).Decode(&apps); err != nil {
		t.Fatalf("decode apps: %v", err)
	}
	if len(apps) == 0 {
		t.Errorf("expected non-empty apps list")
	}

	// 4. Update application
	patchBody := `{"executorTimeoutSecs":60}`
	patchReq, _ := http.NewRequest(http.MethodPatch, ts.URL+"/v2/orgs/local/apps/test-app", strings.NewReader(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatalf("patch app: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("patch app status = %d, want 204 or 200", resp.StatusCode)
	}

	// 5. App versions
	resp, err = http.Get(ts.URL + "/v2/orgs/local/apps/test-app/versions")
	if err != nil {
		t.Fatalf("get versions: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get versions status = %d, want 200", resp.StatusCode)
	}

	// 6. Delete application
	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v2/orgs/local/apps/test-app", nil)
	resp, err = http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete app: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete app status = %d, want 204 or 200", resp.StatusCode)
	}
}

func TestConformance_WorkflowLifecycleAndMutations(t *testing.T) {
	wfUUID := "wf-123"
	statusStr := "SUCCESS"
	nameStr := "testWorkflow"

	routerFn := func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
		switch m := msg.(type) {
		case *protocol.ListWorkflowsRequest:
			return &protocol.ListWorkflowsResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: m.RequestID},
				Output: []protocol.ListWorkflowsResponseBody{
					{WorkflowUUID: wfUUID, Status: &statusStr, WorkflowName: &nameStr},
				},
			}, nil
		case *protocol.GetWorkflowRequest:
			return &protocol.GetWorkflowResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: m.RequestID},
				Output:   &protocol.ListWorkflowsResponseBody{WorkflowUUID: wfUUID, Status: &statusStr, WorkflowName: &nameStr},
			}, nil
		case *protocol.ListStepsRequest:
			stepName := "step-1"
			return &protocol.ListStepsResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeListSteps, RequestID: m.RequestID},
				Output: &[]protocol.WorkflowStepsResponseBody{
					{FunctionName: stepName},
				},
			}, nil
		case *protocol.GetWorkflowEventsRequest:
			return &protocol.GetWorkflowEventsResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowEvents, RequestID: m.RequestID},
				Events:   []protocol.EventOutput{{Key: "k1", Value: "v1"}},
			}, nil
		case *protocol.CancelWorkflowRequest:
			return &protocol.CancelWorkflowResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeCancel, RequestID: m.RequestID},
			}, nil
		case *protocol.ResumeWorkflowRequest:
			return &protocol.ResumeWorkflowResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeResume, RequestID: m.RequestID},
			}, nil
		case *protocol.ForkWorkflowRequest:
			forkedID := "wf-forked-456"
			return &protocol.ForkWorkflowResponse{
				Envelope:      protocol.Envelope{Type: protocol.MessageTypeForkWorkflow, RequestID: m.RequestID},
				NewWorkflowID: &forkedID,
			}, nil
		case *protocol.DeleteWorkflowRequest:
			return &protocol.DeleteWorkflowResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeDelete, RequestID: m.RequestID},
			}, nil
		default:
			return nil, fmt.Errorf("unexpected message: %v", msg.GetMessageType())
		}
	}

	ts, store := setupTestServer(t, routerFn)
	defer ts.Close()

	// Seed application in store
	_, _ = store.UpsertApplication(context.Background(), storegen.UpsertApplicationParams{
		OrganisationID: store.orgs["local"].ID,
		Name:           "test-app",
		Settings:       []byte(`{}`),
	})

	// 1. Workflow search
	searchBody := `{"workflowIds":["wf-123"]}`
	resp, err := http.Post(ts.URL+"/v2/orgs/local/apps/test-app/workflows/search", "application/json", strings.NewReader(searchBody))
	if err != nil {
		t.Fatalf("search workflows: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status = %d, want 200", resp.StatusCode)
	}
	var wfs []gen.Workflow
	if err := json.NewDecoder(resp.Body).Decode(&wfs); err != nil {
		t.Fatalf("decode wfs: %v", err)
	}
	if len(wfs) != 1 || wfs[0].WorkflowId != wfUUID {
		t.Fatalf("unexpected search result: %+v", wfs)
	}

	// 2. Get workflow
	resp, err = http.Get(ts.URL + "/v2/orgs/local/apps/test-app/workflows/" + wfUUID)
	if err != nil {
		t.Fatalf("get workflow: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d, want 200", resp.StatusCode)
	}

	// 3. Workflow steps
	resp, err = http.Get(ts.URL + "/v2/orgs/local/apps/test-app/workflows/" + wfUUID + "/steps")
	if err != nil {
		t.Fatalf("get steps: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("steps status = %d, want 200", resp.StatusCode)
	}

	// 4. Workflow events
	resp, err = http.Get(ts.URL + "/v2/orgs/local/apps/test-app/workflows/" + wfUUID + "/events")
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d, want 200", resp.StatusCode)
	}

	// 5. Cancel workflow
	resp, err = http.Post(ts.URL+"/v2/orgs/local/apps/test-app/workflows/"+wfUUID+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("cancel workflow: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d, want 204 or 200", resp.StatusCode)
	}

	// 6. Resume workflow
	resp, err = http.Post(ts.URL+"/v2/orgs/local/apps/test-app/workflows/"+wfUUID+"/resume", "application/json", nil)
	if err != nil {
		t.Fatalf("resume workflow: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("resume status = %d, want 204 or 200", resp.StatusCode)
	}

	// 7. Fork workflow
	forkReq := `{"newWorkflowId":"wf-forked-456"}`
	resp, err = http.Post(ts.URL+"/v2/orgs/local/apps/test-app/workflows/"+wfUUID+"/fork", "application/json", strings.NewReader(forkReq))
	if err != nil {
		t.Fatalf("fork workflow: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("fork status = %d, want 201 or 200", resp.StatusCode)
	}

	// 8. Delete workflow
	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v2/orgs/local/apps/test-app/workflows/"+wfUUID, nil)
	resp, err = http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete workflow: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want 204 or 200", resp.StatusCode)
	}
}

func TestConformance_QueuesAndSchedules(t *testing.T) {
	routerFn := func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
		switch m := msg.(type) {
		case *protocol.ListQueuesRequest:
			qName := "default"
			return &protocol.ListQueuesResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeListQueues, RequestID: m.RequestID},
				Output:   []protocol.QueueOutput{{Name: qName}},
			}, nil
		case *protocol.GetQueueRequest:
			qName := "default"
			return &protocol.GetQueueResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeGetQueue, RequestID: m.RequestID},
				Output:   &protocol.QueueOutput{Name: qName},
			}, nil
		case *protocol.ListSchedulesRequest:
			schedName := "daily"
			return &protocol.ListSchedulesResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeListSchedules, RequestID: m.RequestID},
				Output:   []protocol.ScheduleOutput{{ScheduleName: schedName}},
			}, nil
		case *protocol.GetScheduleRequest:
			schedName := "daily"
			return &protocol.GetScheduleResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeGetSchedule, RequestID: m.RequestID},
				Output:   &protocol.ScheduleOutput{ScheduleName: schedName},
			}, nil
		case *protocol.PauseScheduleRequest:
			return &protocol.PauseScheduleResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypePauseSchedule, RequestID: m.RequestID},
				Success:  true,
			}, nil
		case *protocol.ResumeScheduleRequest:
			return &protocol.ResumeScheduleResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeResumeSchedule, RequestID: m.RequestID},
				Success:  true,
			}, nil
		case *protocol.TriggerScheduleRequest:
			triggeredID := "wf-triggered-789"
			return &protocol.TriggerScheduleResponse{
				Envelope:   protocol.Envelope{Type: protocol.MessageTypeTriggerSchedule, RequestID: m.RequestID},
				WorkflowID: &triggeredID,
			}, nil
		case *protocol.BackfillScheduleRequest:
			return &protocol.BackfillScheduleResponse{
				Envelope:    protocol.Envelope{Type: protocol.MessageTypeBackfillSchedule, RequestID: m.RequestID},
				WorkflowIDs: []string{"wf-backfill-1"},
			}, nil
		default:
			return nil, fmt.Errorf("unexpected message: %v", msg.GetMessageType())
		}
	}

	ts, store := setupTestServer(t, routerFn)
	defer ts.Close()

	_, _ = store.UpsertApplication(context.Background(), storegen.UpsertApplicationParams{
		OrganisationID: store.orgs["local"].ID,
		Name:           "test-app",
	})

	// 1. List queues
	resp, err := http.Get(ts.URL + "/v2/orgs/local/apps/test-app/queues")
	if err != nil {
		t.Fatalf("list queues: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list queues status = %d, want 200", resp.StatusCode)
	}

	// 2. Get queue
	resp, err = http.Get(ts.URL + "/v2/orgs/local/apps/test-app/queues/default")
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get queue status = %d, want 200", resp.StatusCode)
	}

	// 3. List schedules
	resp, err = http.Get(ts.URL + "/v2/orgs/local/apps/test-app/schedules")
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list schedules status = %d, want 200", resp.StatusCode)
	}

	// 4. Get schedule
	resp, err = http.Get(ts.URL + "/v2/orgs/local/apps/test-app/schedules/daily")
	if err != nil {
		t.Fatalf("get schedule: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get schedule status = %d, want 200", resp.StatusCode)
	}

	// 5. Pause schedule
	resp, err = http.Post(ts.URL+"/v2/orgs/local/apps/test-app/schedules/daily/pause", "application/json", nil)
	if err != nil {
		t.Fatalf("pause schedule: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("pause status = %d, want 204 or 200", resp.StatusCode)
	}

	// 6. Resume schedule
	resp, err = http.Post(ts.URL+"/v2/orgs/local/apps/test-app/schedules/daily/resume", "application/json", nil)
	if err != nil {
		t.Fatalf("resume schedule: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("resume status = %d, want 204 or 200", resp.StatusCode)
	}

	// 7. Trigger schedule (returns 201)
	resp, err = http.Post(ts.URL+"/v2/orgs/local/apps/test-app/schedules/daily/trigger", "application/json", nil)
	if err != nil {
		t.Fatalf("trigger schedule: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("trigger status = %d, want 201 or 200", resp.StatusCode)
	}

	// 8. Backfill schedule
	backfillBody := `{"startTime":"2026-01-01T00:00:00Z","endTime":"2026-01-02T00:00:00Z"}`
	resp, err = http.Post(ts.URL+"/v2/orgs/local/apps/test-app/schedules/daily/backfill", "application/json", strings.NewReader(backfillBody))
	if err != nil {
		t.Fatalf("backfill schedule: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("backfill status = %d, want 200", resp.StatusCode)
	}
}

func TestConformance_TokensAndPermissions(t *testing.T) {
	ts, _ := setupTestServer(t, nil)
	defer ts.Close()

	// 1. List permissions
	resp, err := http.Get(ts.URL + "/v2/orgs/local/permissions")
	if err != nil {
		t.Fatalf("list permissions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("permissions status = %d, want 200", resp.StatusCode)
	}
	var perms []string
	if err := json.NewDecoder(resp.Body).Decode(&perms); err != nil {
		t.Fatalf("decode perms: %v", err)
	}
	if len(perms) == 0 {
		t.Errorf("expected permissions list")
	}

	// 2. Create token
	createTokenBody := `{"permissions":["admin"]}`
	resp, err = http.Post(ts.URL+"/v2/orgs/local/tokens/conformance-key", "application/json", strings.NewReader(createTokenBody))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create token status = %d, want 201 or 200", resp.StatusCode)
	}
	var tokenResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	tokenSecret, ok := tokenResp["token"].(string)
	if !ok || !strings.HasPrefix(tokenSecret, auth.KeyPrefix) {
		t.Errorf("token secret %q lacks %q prefix", tokenSecret, auth.KeyPrefix)
	}

	// 3. List tokens
	resp, err = http.Get(ts.URL + "/v2/orgs/local/tokens")
	if err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tokens status = %d, want 200", resp.StatusCode)
	}

	// 4. Delete token
	delReq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v2/orgs/local/tokens/conformance-key", nil)
	resp, err = http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete token: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete token status = %d, want 204 or 200", resp.StatusCode)
	}
}

func TestConformance_ProblemDetailsFormat(t *testing.T) {
	routerFn := func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
		if appName == "no-executor-app" {
			return nil, router.ErrNoLiveExecutor
		}
		if appName == "timeout-app" {
			return nil, router.ErrExecutorTimeout
		}
		return nil, router.ErrAppNotFound
	}

	ts, store := setupTestServer(t, routerFn)
	defer ts.Close()

	_, _ = store.UpsertApplication(context.Background(), storegen.UpsertApplicationParams{
		OrganisationID: store.orgs["local"].ID,
		Name:           "no-executor-app",
	})
	_, _ = store.UpsertApplication(context.Background(), storegen.UpsertApplicationParams{
		OrganisationID: store.orgs["local"].ID,
		Name:           "timeout-app",
	})

	cases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "no live executor returns 503 Problem",
			path:       "/v2/orgs/local/apps/no-executor-app/workflows/search",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "executor timeout returns 504 Problem",
			path:       "/v2/orgs/local/apps/timeout-app/workflows/search",
			wantStatus: http.StatusGatewayTimeout,
		},
		{
			name:       "missing entity returns 404 Problem",
			path:       "/v2/orgs/local/apps/nonexistent-app/workflows/search",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(ts.URL+tc.path, "application/json", strings.NewReader(`{}`))
			if err != nil {
				t.Fatalf("post request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}

			body, _ := io.ReadAll(resp.Body)
			var prob problem.Problem
			if err := json.Unmarshal(body, &prob); err != nil {
				t.Fatalf("failed to decode RFC 9457 problem JSON: %v, body: %s", err, string(body))
			}
			if prob.Status != tc.wantStatus {
				t.Errorf("problem status = %d, want %d", prob.Status, tc.wantStatus)
			}
			if prob.Title == "" {
				t.Errorf("problem title is empty")
			}
			if prob.Detail == "" {
				t.Errorf("problem detail is empty")
			}
		})
	}
}

func TestConformance_InvalidJSONReturnsProblem400(t *testing.T) {
	ts, _ := setupTestServer(t, nil)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v2/orgs/local/apps/test-app/workflows/search", "application/json", bytes.NewReader([]byte("{invalid-json")))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}

	var prob problem.Problem
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("failed to decode problem json: %v", err)
	}
	if prob.Status != http.StatusBadRequest {
		t.Errorf("problem status = %d, want 400", prob.Status)
	}
}
