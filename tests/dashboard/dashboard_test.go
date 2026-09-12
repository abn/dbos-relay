package dashboard_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	if !ok {
		return storegen.ApiKey{}, errors.New("key not found")
	}
	return key, nil
}

func (s *dashboardTestStore) ListAPIKeys(_ context.Context, orgID pgtype.UUID) ([]storegen.ApiKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []storegen.ApiKey
	for _, k := range s.keys {
		if k.OrganisationID == orgID {
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

func (s *dashboardTestStore) CreateUser(ctx context.Context, arg storegen.CreateUserParams) (storegen.User, error) { return storegen.User{}, nil }
func (s *dashboardTestStore) UpsertUser(ctx context.Context, arg storegen.UpsertUserParams) (storegen.User, error) { return storegen.User{}, nil }
func (s *dashboardTestStore) GetUserBySubject(ctx context.Context, subject string) (storegen.User, error) { return storegen.User{}, nil }
func (s *dashboardTestStore) GetUserByUsername(ctx context.Context, username string) (storegen.User, error) { return storegen.User{}, nil }
func (s *dashboardTestStore) GetUserByID(ctx context.Context, id pgtype.UUID) (storegen.User, error) { return storegen.User{}, nil }
func (s *dashboardTestStore) ListMembersByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]storegen.ListMembersByOrganisationRow, error) { return nil, nil }
func (s *dashboardTestStore) GetMember(ctx context.Context, arg storegen.GetMemberParams) (storegen.GetMemberRow, error) { return storegen.GetMemberRow{}, nil }
func (s *dashboardTestStore) UpsertMemberRole(ctx context.Context, arg storegen.UpsertMemberRoleParams) (storegen.OrganisationMember, error) { return storegen.OrganisationMember{}, nil }
func (s *dashboardTestStore) RemoveMember(ctx context.Context, arg storegen.RemoveMemberParams) (storegen.OrganisationMember, error) { return storegen.OrganisationMember{}, nil }
func (s *dashboardTestStore) GetUserPrimaryOrganisation(ctx context.Context, userID pgtype.UUID) (storegen.GetUserPrimaryOrganisationRow, error) { return storegen.GetUserPrimaryOrganisationRow{}, nil }
func (s *dashboardTestStore) ListRoles(ctx context.Context, organisationID pgtype.UUID) ([]storegen.Role, error) { return nil, nil }
func (s *dashboardTestStore) GetRole(ctx context.Context, arg storegen.GetRoleParams) (storegen.Role, error) { return storegen.Role{}, nil }
func (s *dashboardTestStore) CreateRole(ctx context.Context, arg storegen.CreateRoleParams) (storegen.Role, error) { return storegen.Role{}, nil }
func (s *dashboardTestStore) DeleteRole(ctx context.Context, arg storegen.DeleteRoleParams) (storegen.Role, error) { return storegen.Role{}, nil }
func (s *dashboardTestStore) ListDomainClaims(ctx context.Context, organisationID pgtype.UUID) ([]storegen.DomainClaim, error) { return nil, nil }
func (s *dashboardTestStore) GetDomainClaim(ctx context.Context, domain string) (storegen.DomainClaim, error) { return storegen.DomainClaim{}, nil }
func (s *dashboardTestStore) CreateDomainClaim(ctx context.Context, arg storegen.CreateDomainClaimParams) (storegen.DomainClaim, error) { return storegen.DomainClaim{}, nil }
func (s *dashboardTestStore) DeleteDomainClaim(ctx context.Context, arg storegen.DeleteDomainClaimParams) (storegen.DomainClaim, error) { return storegen.DomainClaim{}, nil }
func (s *dashboardTestStore) CreateAuditLog(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) { return storegen.AuditLog{}, nil }
func (s *dashboardTestStore) ListAuditLogs(ctx context.Context, arg storegen.ListAuditLogsParams) ([]storegen.AuditLog, error) { return nil, nil }

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
		if !strings.Contains(string(bodyBytes), "Relay Dashboard") {
			t.Errorf("expected SPA route %s to serve index.html", sp)
		}
	}
}

func TestDashboard_EndToEndWithFakeExecutor(t *testing.T) {
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

	// Allow registration
	time.Sleep(50 * time.Millisecond)

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
