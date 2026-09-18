package dashboard_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/dashboard"
	"github.com/abn/relay/internal/fakeexecutor"
	"github.com/abn/relay/internal/hub"
	"github.com/abn/relay/internal/metrics"
	"github.com/abn/relay/internal/router"
	storegen "github.com/abn/relay/internal/store/gen"
)

type dashboardTestStore struct {
	mu        sync.Mutex
	orgs      map[string]storegen.Organisation
	apps      map[string]storegen.Application
	executors map[string]storegen.Executor
	keys      map[string]storegen.ApiKey
}

func newDashboardTestStore() *dashboardTestStore {
	return &dashboardTestStore{
		orgs:      make(map[string]storegen.Organisation),
		apps:      make(map[string]storegen.Application),
		executors: make(map[string]storegen.Executor),
		keys:      make(map[string]storegen.ApiKey),
	}
}

func (s *dashboardTestStore) Ping(_ context.Context) error { return nil }

func (s *dashboardTestStore) GetOrganisationByName(_ context.Context, name string) (storegen.Organisation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	org, ok := s.orgs[name]
	if !ok {
		return storegen.Organisation{}, errors.New("org not found")
	}
	return org, nil
}

func (s *dashboardTestStore) GetApplicationByName(_ context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	app, ok := s.apps[arg.Name]
	if !ok {
		return storegen.Application{}, errors.New("app not found")
	}
	return app, nil
}

func (s *dashboardTestStore) ListApplicationsByOrganisation(_ context.Context, orgID pgtype.UUID) ([]storegen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []storegen.Application
	for _, app := range s.apps {
		if app.OrganisationID == orgID {
			res = append(res, app)
		}
	}
	return res, nil
}

func (s *dashboardTestStore) UpsertApplication(_ context.Context, arg storegen.UpsertApplicationParams) (storegen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	app := storegen.Application{
		ID:             pgtype.UUID{Bytes: [16]byte{2}, Valid: true},
		OrganisationID: arg.OrganisationID,
		Name:           arg.Name,
		Settings:       arg.Settings,
	}
	s.apps[arg.Name] = app
	return app, nil
}

func (s *dashboardTestStore) UpdateApplicationSettings(_ context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	app, ok := s.apps[arg.Name]
	if !ok {
		return storegen.Application{}, errors.New("app not found")
	}
	app.Settings = arg.Settings
	s.apps[arg.Name] = app
	return app, nil
}

func (s *dashboardTestStore) DeleteApplication(_ context.Context, arg storegen.DeleteApplicationParams) (storegen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	app, ok := s.apps[arg.Name]
	if !ok {
		return storegen.Application{}, errors.New("app not found")
	}
	delete(s.apps, arg.Name)
	return app, nil
}

func (s *dashboardTestStore) CreateApplication(_ context.Context, arg storegen.CreateApplicationParams) (storegen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	app := storegen.Application{
		ID:             id,
		OrganisationID: arg.OrganisationID,
		Name:           arg.Name,
		Settings:       arg.Settings,
	}
	s.apps[arg.Name] = app
	return app, nil
}

func (s *dashboardTestStore) ListExecutorsByApplication(_ context.Context, appID pgtype.UUID) ([]storegen.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []storegen.Executor
	for _, e := range s.executors {
		if e.ApplicationID == appID {
			res = append(res, e)
		}
	}
	return res, nil
}

func (s *dashboardTestStore) ListConnectedExecutorsByApplication(_ context.Context, appID pgtype.UUID) ([]storegen.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []storegen.Executor
	for _, e := range s.executors {
		if e.ApplicationID == appID && e.Status == "connected" {
			res = append(res, e)
		}
	}
	return res, nil
}

func (s *dashboardTestStore) UpsertExecutor(_ context.Context, arg storegen.UpsertExecutorParams) (storegen.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := storegen.Executor{
		ApplicationID:      arg.ApplicationID,
		ExecutorID:         arg.ExecutorID,
		ApplicationVersion: arg.ApplicationVersion,
		Hostname:           arg.Hostname,
		Metadata:           arg.Metadata,
		Status:             "connected",
	}
	s.executors[arg.ExecutorID] = e
	return e, nil
}

func (s *dashboardTestStore) DisconnectExecutor(_ context.Context, arg storegen.DisconnectExecutorParams) (storegen.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.executors[arg.ExecutorID]
	if !ok {
		return storegen.Executor{}, errors.New("not found")
	}
	e.Status = "disconnected"
	s.executors[arg.ExecutorID] = e
	return e, nil
}

func (s *dashboardTestStore) GetAPIKeyByLookup(_ context.Context, lookup string) (storegen.ApiKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[lookup]
	if !ok || !key.RevokedAt.Time.IsZero() {
		return storegen.ApiKey{}, errors.New("key not found")
	}
	return key, nil
}

func (s *dashboardTestStore) ListAPIKeys(_ context.Context, orgID pgtype.UUID) ([]storegen.ApiKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []storegen.ApiKey
	for _, k := range s.keys {
		if k.OrganisationID == orgID && k.RevokedAt.Time.IsZero() {
			res = append(res, k)
		}
	}
	return res, nil
}

func (s *dashboardTestStore) CreateAPIKey(_ context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := storegen.ApiKey{
		ID:               pgtype.UUID{Bytes: [16]byte{3}, Valid: true},
		OrganisationID:   arg.OrganisationID,
		Name:             arg.Name,
		Lookup:           arg.Lookup,
		KeyHash:          arg.KeyHash,
		ApplicationNames: arg.ApplicationNames,
		Permissions:      arg.Permissions,
	}
	s.keys[arg.Lookup] = key
	return key, nil
}

func (s *dashboardTestStore) RevokeAPIKey(_ context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error) {
	return storegen.ApiKey{}, nil
}

func (s *dashboardTestStore) UpsertOrganisation(_ context.Context, name string) (storegen.Organisation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	org := storegen.Organisation{
		ID:   pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Name: name,
	}
	s.orgs[name] = org
	return org, nil
}

func (s *dashboardTestStore) CreateAlertingRule(_ context.Context, arg storegen.CreateAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{ID: pgtype.UUID{Bytes: [16]byte{4}, Valid: true}}, nil
}

func (s *dashboardTestStore) GetAlertingRule(_ context.Context, _ storegen.GetAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{}, errors.New("rule not found")
}

func (s *dashboardTestStore) ListAlertingRulesByApplication(_ context.Context, _ pgtype.UUID) ([]storegen.AlertingRule, error) {
	return nil, nil
}

func (s *dashboardTestStore) DeleteAlertingRule(_ context.Context, _ storegen.DeleteAlertingRuleParams) (int64, error) {
	return 1, nil
}

func (s *dashboardTestStore) ListAllApplications(_ context.Context) ([]storegen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []storegen.Application
	for _, app := range s.apps {
		res = append(res, app)
	}
	return res, nil
}

func (s *dashboardTestStore) CreateUser(ctx context.Context, arg storegen.CreateUserParams) (storegen.User, error) {
	return storegen.User{}, nil
}
func (s *dashboardTestStore) UpsertUser(ctx context.Context, arg storegen.UpsertUserParams) (storegen.User, error) {
	return storegen.User{}, nil
}
func (s *dashboardTestStore) GetUserBySubject(ctx context.Context, subject string) (storegen.User, error) {
	return storegen.User{}, nil
}
func (s *dashboardTestStore) GetUserByUsername(ctx context.Context, username string) (storegen.User, error) {
	return storegen.User{}, nil
}
func (s *dashboardTestStore) GetUserByID(ctx context.Context, id pgtype.UUID) (storegen.User, error) {
	return storegen.User{}, nil
}
func (s *dashboardTestStore) ListMembersByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]storegen.ListMembersByOrganisationRow, error) {
	return nil, nil
}
func (s *dashboardTestStore) GetMember(ctx context.Context, arg storegen.GetMemberParams) (storegen.GetMemberRow, error) {
	return storegen.GetMemberRow{}, nil
}
func (s *dashboardTestStore) UpsertMemberRole(ctx context.Context, arg storegen.UpsertMemberRoleParams) (storegen.OrganisationMember, error) {
	return storegen.OrganisationMember{}, nil
}
func (s *dashboardTestStore) RemoveMember(ctx context.Context, arg storegen.RemoveMemberParams) (storegen.OrganisationMember, error) {
	return storegen.OrganisationMember{}, nil
}
func (s *dashboardTestStore) GetUserPrimaryOrganisation(ctx context.Context, userID pgtype.UUID) (storegen.GetUserPrimaryOrganisationRow, error) {
	return storegen.GetUserPrimaryOrganisationRow{}, nil
}
func (s *dashboardTestStore) ListRoles(ctx context.Context, organisationID pgtype.UUID) ([]storegen.Role, error) {
	return nil, nil
}
func (s *dashboardTestStore) GetRole(ctx context.Context, arg storegen.GetRoleParams) (storegen.Role, error) {
	return storegen.Role{}, nil
}
func (s *dashboardTestStore) CreateRole(ctx context.Context, arg storegen.CreateRoleParams) (storegen.Role, error) {
	return storegen.Role{}, nil
}
func (s *dashboardTestStore) DeleteRole(ctx context.Context, arg storegen.DeleteRoleParams) (storegen.Role, error) {
	return storegen.Role{}, nil
}
func (s *dashboardTestStore) ListDomainClaims(ctx context.Context, organisationID pgtype.UUID) ([]storegen.DomainClaim, error) {
	return nil, nil
}
func (s *dashboardTestStore) GetDomainClaim(ctx context.Context, domain string) (storegen.DomainClaim, error) {
	return storegen.DomainClaim{}, nil
}
func (s *dashboardTestStore) CreateDomainClaim(ctx context.Context, arg storegen.CreateDomainClaimParams) (storegen.DomainClaim, error) {
	return storegen.DomainClaim{}, nil
}
func (s *dashboardTestStore) DeleteDomainClaim(ctx context.Context, arg storegen.DeleteDomainClaimParams) (storegen.DomainClaim, error) {
	return storegen.DomainClaim{}, nil
}
func (s *dashboardTestStore) CreateAuditLog(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
	return storegen.AuditLog{}, nil
}
func (s *dashboardTestStore) ListAuditLogs(ctx context.Context, arg storegen.ListAuditLogsParams) ([]storegen.AuditLog, error) {
	return nil, nil
}

func TestDashboard_LiveServerRootServesUI(t *testing.T) {
	store := newDashboardTestStore()
	cfg := &config.Config{ExecutorDeadline: 5 * time.Second}
	h := hub.New(store, cfg, nil)
	defer func() { _ = h.Close() }()

	r := router.New(store, h)
	apiServer := api.NewServer(r, store, nil)
	apiHandler := api.NewHandler(store, apiServer)
	dashHandler := dashboard.Handler(apiHandler)

	mux := http.NewServeMux()
	mux.Handle("/", dashHandler)
	mux.Handle("/websocket/", h)
	mux.Handle("/v1/metrics", api.AuthMiddleware(apiServer)(metrics.NewHandler(store)))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 1. GET / serves the dashboard HTML
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type text/html for /, got %q", ct)
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	if !strings.Contains(body, "Relay Dashboard") {
		t.Errorf("expected HTML body to contain 'Relay Dashboard'")
	}
	if !strings.Contains(body, `<div id="app"></div>`) {
		t.Errorf("expected HTML body to contain app root element")
	}
}

func TestDashboard_StaticAssetsAndSPARoutes(t *testing.T) {
	store := newDashboardTestStore()
	cfg := &config.Config{ExecutorDeadline: 5 * time.Second}
	h := hub.New(store, cfg, nil)
	defer func() { _ = h.Close() }()

	r := router.New(store, h)
	apiServer := api.NewServer(r, store, nil)
	apiHandler := api.NewHandler(store, apiServer)
	dashHandler := dashboard.Handler(apiHandler)

	mux := http.NewServeMux()
	mux.Handle("/", dashHandler)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Static Assets
	assets := []struct {
		path        string
		contentType string
	}{
		{"/assets/app.js", "javascript"},
		{"/assets/app.css", "text/css"},
		{"/assets/favicon.svg", "image/svg+xml"},
	}

	for _, a := range assets {
		resp, err := http.Get(ts.URL + a.path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", a.path, err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200 for %s, got %d", a.path, resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, a.contentType) {
			t.Errorf("expected content type %s for %s, got %s", a.contentType, a.path, ct)
		}
	}

	// SPA Routing Fallback
	spaPaths := []string{
		"/fleet",
		"/workflows",
		"/workflows/wf-test-uuid-99",
		"/queues",
		"/schedules",
		"/alerting",
		"/keys",
	}

	for _, sp := range spaPaths {
		resp, err := http.Get(ts.URL + sp)
		if err != nil {
			t.Fatalf("GET %s failed: %v", sp, err)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200 for SPA route %s, got %d", sp, resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected content type text/html for SPA route %s, got %s", sp, ct)
		}
		if !strings.Contains(string(bodyBytes), "Relay Dashboard") {
			t.Errorf("expected SPA route %s to serve index.html", sp)
		}
	}
}

func TestDashboard_EndToEndWithFakeExecutor(t *testing.T) {
	t.Logf("counterparty: internal/fakeexecutor (in-process stand-in for a DBOS SDK executor)")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := newDashboardTestStore()
	orgID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	appID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	appName := "order-service"
	orgName := "default"

	store.orgs[orgName] = storegen.Organisation{ID: orgID, Name: orgName}
	store.apps[appName] = storegen.Application{ID: appID, OrganisationID: orgID, Name: appName}

	plainKey, rec, err := auth.Mint()
	if err != nil {
		t.Fatalf("Mint failed: %v", err)
	}
	store.keys[rec.Lookup] = storegen.ApiKey{
		OrganisationID:   orgID,
		Name:             "dash-key",
		Lookup:           rec.Lookup,
		KeyHash:          rec.Hash,
		ApplicationNames: []string{appName},
	}

	cfg := &config.Config{ExecutorDeadline: 5 * time.Second}
	h := hub.New(store, cfg, nil)
	defer func() { _ = h.Close() }()

	r := router.New(store, h)
	apiServer := api.NewServer(r, store, nil)
	apiHandler := api.NewHandler(store, apiServer)
	dashHandler := dashboard.Handler(apiHandler)

	mux := http.NewServeMux()
	mux.Handle("/", dashHandler)
	mux.Handle("/websocket/", h)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Connect Fake Executor
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	exec := fakeexecutor.New(fakeexecutor.Options{
		URL:                wsURL,
		AppName:            appName,
		ConductorKey:       plainKey,
		ExecutorID:         "exec-dash-1",
		ApplicationVersion: "v1.0.0",
		Hostname:           "worker-node-1",
		Language:           "typescript",
	})

	if err := exec.Connect(ctx); err != nil {
		t.Fatalf("FakeExecutor connect failed: %v", err)
	}
	defer func() { _ = exec.Close() }()

	go func() {
		_ = exec.Run(ctx)
	}()

	// Poll until executor appears in store
	deadline := time.Now().Add(5 * time.Second)
	var registered bool
	for time.Now().Before(deadline) {
		store.mu.Lock()
		_, registered = store.executors["exec-dash-1"]
		store.mu.Unlock()
		if registered {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !registered {
		t.Fatal("timed out waiting for executor registration")
	}

	// Query executor list from the REST endpoint used by the Dashboard UI
	execURL := ts.URL + "/v2/orgs/" + orgName + "/apps/" + appName + "/executors"
	resp, err := http.Get(execURL)
	if err != nil {
		t.Fatalf("GET executors failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var execList []struct {
		ExecutorID string `json:"executorId"`
		Status     string `json:"status"`
		Hostname   string `json:"hostname"`
		Language   string `json:"language"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&execList); err != nil {
		t.Fatalf("decoding executors list: %v", err)
	}

	if len(execList) == 0 {
		t.Fatalf("expected at least 1 registered executor")
	}

	found := false
	for _, e := range execList {
		if e.ExecutorID == "exec-dash-1" {
			found = true
			if e.Status != "HEALTHY" {
				t.Errorf("expected executor status HEALTHY, got %s", e.Status)
			}
			if e.Hostname != "worker-node-1" {
				t.Errorf("expected hostname worker-node-1, got %s", e.Hostname)
			}
			if e.Language != "typescript" {
				t.Errorf("expected language typescript, got %s", e.Language)
			}
		}
	}
	if !found {
		t.Errorf("exec-dash-1 not found in executor list")
	}
}

func (m *dashboardTestStore) TouchAPIKeyLastUsed(ctx context.Context, id pgtype.UUID) error {
	return nil
}

func TestDashboard_BundleParses(t *testing.T) {
	cmd := exec.Command("node", "--check", "../../internal/dashboard/dist/assets/app.js")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --check failed: %v\nOutput: %s", err, string(output))
	}
}

func TestDashboard_URLTemplatesMatchOpenAPI(t *testing.T) {
	clientBytes, err := os.ReadFile("../../console/src/lib/api/client.ts")
	if err != nil {
		t.Fatalf("reading client.ts: %v", err)
	}

	specBytes, err := os.ReadFile("../../api/spec/openapi-3.0.json")
	if err != nil {
		t.Fatalf("reading openapi-3.0.json: %v", err)
	}

	var spec struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		t.Fatalf("unmarshaling openapi spec: %v", err)
	}

	re := regexp.MustCompile("`(/v2/[^`]+)`")
	matches := re.FindAllStringSubmatch(string(clientBytes), -1)
	if len(matches) == 0 {
		t.Fatalf("expected to find /v2/ templates in client.ts")
	}

	placeholderRe := regexp.MustCompile(`\$\{encodeURIComponent\(([^)]+)\)\}`)

	for _, m := range matches {
		rawPath := m[1]
		if idx := strings.Index(rawPath, "${qs"); idx != -1 {
			rawPath = rawPath[:idx]
		}
		if idx := strings.Index(rawPath, "?"); idx != -1 {
			rawPath = rawPath[:idx]
		}
		normalized := placeholderRe.ReplaceAllString(rawPath, "{$1}")

		normalized = strings.ReplaceAll(normalized, "{orgName}", "{orgName}")
		normalized = strings.ReplaceAll(normalized, "{appName}", "{appName}")
		normalized = strings.ReplaceAll(normalized, "{workflowId}", "{workflowId}")
		normalized = strings.ReplaceAll(normalized, "{queueName}", "{queueName}")
		normalized = strings.ReplaceAll(normalized, "{scheduleName}", "{scheduleName}")
		normalized = strings.ReplaceAll(normalized, "{ruleId}", "{ruleId}")
		normalized = strings.ReplaceAll(normalized, "{name}", "{tokenName}")
		normalized = strings.ReplaceAll(normalized, "{org}", "{orgName}")
		normalized = strings.ReplaceAll(normalized, "{app}", "{appName}")
		normalized = strings.ReplaceAll(normalized, "{id}", "{workflowId}")

		if strings.Contains(normalized, "/restart") {
			t.Errorf("found dead /restart template: %s", normalized)
		}

		// Allow Relay extension endpoints (e.g. SSE event streaming)
		if strings.HasSuffix(normalized, "/events") {
			continue
		}

		if _, exists := spec.Paths[normalized]; !exists {
			t.Errorf("template %s (normalized: %s) does not exist in OpenAPI spec", m[1], normalized)
		}
	}
}

func TestDashboard_TokenContractParity(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/orgs/{orgName}/tokens", func(w http.ResponseWriter, r *http.Request) {
		tokens := []map[string]any{
			{
				"tokenName":   "deploy-key",
				"createdAt":   "2026-09-12T12:00:00Z",
				"permissions": []string{"application.read", "application.write"},
				"appIds":      []string{"order-service"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokens)
	})

	var revokedToken string
	mux.HandleFunc("DELETE /v2/orgs/{orgName}/tokens/{tokenName}", func(w http.ResponseWriter, r *http.Request) {
		revokedToken = r.PathValue("tokenName")
		w.WriteHeader(http.StatusNoContent)
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 1. Fetch tokens and verify wire format matches Token schema
	resp, err := http.Get(ts.URL + "/v2/orgs/default/tokens")
	if err != nil {
		t.Fatalf("GET tokens failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var tokens []struct {
		TokenName   string   `json:"tokenName"`
		CreatedAt   string   `json:"createdAt"`
		Permissions []string `json:"permissions"`
		AppIds      []string `json:"appIds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		t.Fatalf("decoding tokens: %v", err)
	}

	if len(tokens) != 1 || tokens[0].TokenName != "deploy-key" || len(tokens[0].AppIds) != 1 || tokens[0].AppIds[0] != "order-service" {
		t.Fatalf("unexpected token data: %+v", tokens)
	}

	// 2. Revoke token by name
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v2/orgs/default/tokens/"+tokens[0].TokenName, nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE token failed: %v", err)
	}
	_ = delResp.Body.Close()

	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 No Content, got %d", delResp.StatusCode)
	}
	if revokedToken != "deploy-key" {
		t.Fatalf("expected revokedToken deploy-key, got %q", revokedToken)
	}

	// 3. Verify built bundle reads tokenName and appIds
	bundleBytes, err := os.ReadFile("../../internal/dashboard/dist/assets/app.js")
	if err != nil {
		t.Fatalf("reading app.js bundle: %v", err)
	}
	bundleStr := string(bundleBytes)
	if !strings.Contains(bundleStr, "tokenName") {
		t.Errorf("bundle should reference tokenName")
	}
	if !strings.Contains(bundleStr, "appIds") {
		t.Errorf("bundle should reference appIds")
	}
}

func TestDashboard_ForkWorkflowContract(t *testing.T) {
	var requestedStartStep int
	var receivedAuthHeader string

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/orgs/{orgName}/apps/{appName}/workflows/{workflowId}/fork", func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		var body struct {
			StartStep int `json:"startStep"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		requestedStartStep = body.StartStep

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"workflowId":"wf-forked-uuid-42"}`))
	})

	dashHandler := dashboard.Handler(mux)
	ts := httptest.NewServer(dashHandler)
	defer ts.Close()

	forkURL := ts.URL + "/v2/orgs/default/apps/test-app/workflows/wf-orig-1/fork"
	req, _ := http.NewRequest(http.MethodPost, forkURL, strings.NewReader(`{"startStep":0}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer dbos_sec_testtoken")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST fork failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}
	if receivedAuthHeader != "Bearer dbos_sec_testtoken" {
		t.Errorf("expected Authorization header passed through, got %q", receivedAuthHeader)
	}
	if requestedStartStep != 0 {
		t.Errorf("expected startStep 0, got %d", requestedStartStep)
	}

	var res struct {
		WorkflowID string `json:"workflowId"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	if res.WorkflowID != "wf-forked-uuid-42" {
		t.Errorf("expected workflowId wf-forked-uuid-42, got %q", res.WorkflowID)
	}
}

func TestDashboard_AuthenticationFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/orgs/default/apps", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"about:blank","title":"Unauthorized","status":401,"detail":"Authorization header required"}`))
			return
		}
		if authHeader != "Bearer dbos_sec_validkey" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"name":"demo-app"}]`))
	})

	dashHandler := dashboard.Handler(mux)
	ts := httptest.NewServer(dashHandler)
	defer ts.Close()

	// 1. Unauthenticated request to /v2/ returns 401
	unauthResp, err := http.Get(ts.URL + "/v2/orgs/default/apps")
	if err != nil {
		t.Fatalf("GET /v2 failed: %v", err)
	}
	_ = unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for unauthenticated /v2, got %d", unauthResp.StatusCode)
	}

	// 2. Authenticated request with Bearer key returns 200
	authReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/v2/orgs/default/apps", nil)
	authReq.Header.Set("Authorization", "Bearer dbos_sec_validkey")
	authResp, err := http.DefaultClient.Do(authReq)
	if err != nil {
		t.Fatalf("GET with auth failed: %v", err)
	}
	_ = authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for authenticated /v2, got %d", authResp.StatusCode)
	}

	// 3. Static asset loads without auth and includes auth support in bundle
	assetResp, err := http.Get(ts.URL + "/assets/app.js")
	if err != nil {
		t.Fatalf("GET app.js failed: %v", err)
	}
	defer func() { _ = assetResp.Body.Close() }()
	if assetResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for app.js, got %d", assetResp.StatusCode)
	}

	bundleBytes, err := io.ReadAll(assetResp.Body)
	if err != nil {
		t.Fatalf("reading app.js body: %v", err)
	}
	bundle := string(bundleBytes)
	if !strings.Contains(bundle, "relay_api_key") {
		t.Errorf("bundle should support relay_api_key session storage")
	}
	if !strings.Contains(bundle, "setApiKey") {
		t.Errorf("bundle should include setApiKey")
	}
	if !strings.Contains(bundle, "renderAuthRequired") {
		t.Errorf("bundle should include renderAuthRequired")
	}
}

func TestDashboard_ChildWorkflowXSSSanitization(t *testing.T) {
	bundleBytes, err := os.ReadFile("../../internal/dashboard/dist/assets/app.js")
	if err != nil {
		t.Fatalf("reading bundle: %v", err)
	}
	bundle := string(bundleBytes)

	// Ensure zero inline onclick handlers exist in the compiled bundle
	if strings.Contains(bundle, "onclick=") {
		t.Errorf("compiled bundle contains inline onclick= handlers")
	}

	// Verify child workflow navigation uses data-action rather than inline script
	if !strings.Contains(bundle, "data-action=\\\"viewChildWorkflow\\\"") && !strings.Contains(bundle, `data-action="viewChildWorkflow"`) {
		t.Errorf("bundle should use data-action for viewChildWorkflow")
	}
	if !strings.Contains(bundle, "data-child-wf-id") {
		t.Errorf("bundle should use data-child-wf-id")
	}
}

func TestDashboard_NoConnectedExecutorsState(t *testing.T) {
	bundleBytes, err := os.ReadFile("../../internal/dashboard/dist/assets/app.js")
	if err != nil {
		t.Fatalf("reading bundle: %v", err)
	}
	bundle := string(bundleBytes)

	if !strings.Contains(bundle, "isNoExecutorError") {
		t.Errorf("bundle should include isNoExecutorError helper")
	}
	if !strings.Contains(bundle, "renderNoExecutorState") {
		t.Errorf("bundle should include renderNoExecutorState")
	}
	if !strings.Contains(bundle, "No Connected Executors") {
		t.Errorf("bundle should include 'No Connected Executors' friendly notice")
	}
}

func TestDashboard_FleetWideAndPerSectionFiltering(t *testing.T) {
	bundleBytes, err := os.ReadFile("../../internal/dashboard/dist/assets/app.js")
	if err != nil {
		t.Fatalf("reading bundle: %v", err)
	}
	bundle := string(bundleBytes)

	// Ensure fleet-wide option and single authoritative title bar filter exist
	if !strings.Contains(bundle, "All Applications") {
		t.Errorf("bundle should include 'All Applications' option")
	}
	if !strings.Contains(bundle, "header-app-select") {
		t.Errorf("bundle should include header-app-select in title bar")
	}
	if !strings.Contains(bundle, "Show All Applications") {
		t.Errorf("bundle should include 'Show All Applications' reset button")
	}
	if !strings.Contains(bundle, "workflowAppMap") {
		t.Errorf("bundle should include workflowAppMap for cross-app workflow routing")
	}
}
