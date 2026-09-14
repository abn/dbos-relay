package metrics_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/metrics"
	"github.com/abn/relay/internal/store/gen"
)

type mockMetricsStore struct {
	apps      []gen.Application
	executors map[pgtype.UUID][]gen.Executor
	key       gen.ApiKey
}

func (m *mockMetricsStore) ListAllApplications(ctx context.Context) ([]gen.Application, error) {
	return m.apps, nil
}

func (m *mockMetricsStore) ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error) {
	return m.executors[appID], nil
}

func (m *mockMetricsStore) GetAPIKeyByLookup(ctx context.Context, lookup string) (gen.ApiKey, error) {
	if m.key.Lookup == lookup {
		return m.key, nil
	}
	return gen.ApiKey{}, http.ErrNoCookie
}

func TestMetricsEndpoint_Scrape(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	store := &mockMetricsStore{
		apps: []gen.Application{
			{
				ID:   appID,
				Name: "test-app",
			},
		},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {
				{
					ApplicationID:      appID,
					ExecutorID:         "exec-1",
					Status:             "connected",
					ApplicationVersion: "v1.0.0",
				},
				{
					ApplicationID:      appID,
					ExecutorID:         "exec-2",
					Status:             "connected",
					ApplicationVersion: "v1.0.0",
				},
				{
					ApplicationID:      appID,
					ExecutorID:         "exec-3",
					Status:             "disconnected",
					ApplicationVersion: "v1.0.0",
				},
			},
		},
		key: gen.ApiKey{
			Permissions: []string{"application.read"},
		},
	}

	handler := metrics.NewHandler(store)

	t.Run("ValidScrapeWithToken", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/metrics", nil)
		ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
			Subject:          "test",
			IsAdmin:          true,
			IsAPIKey:         true,
			ApplicationNames: []string{"test-app"},
		})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		body := w.Body.String()
		if !strings.Contains(body, "# TYPE dbos_conductor_v1_executor_count gauge") {
			t.Errorf("expected metric type declaration, got:\n%s", body)
		}
		if !strings.Contains(body, `dbos_conductor_v1_executor_count{application="test-app",application_version="v1.0.0",status="HEALTHY"} 2`) {
			t.Errorf("expected healthy executor count 2, got:\n%s", body)
		}
		if !strings.Contains(body, `dbos_conductor_v1_executor_count{application="test-app",application_version="v1.0.0",status="DISCONNECTED"} 1`) {
			t.Errorf("expected disconnected executor count 1, got:\n%s", body)
		}
	})

	t.Run("MissingIdentityRejected", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/metrics", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("FilterByApplication", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/metrics?applications=other-app", nil)
		ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
			Subject:          "test",
			IsAdmin:          true,
			IsAPIKey:         true,
			ApplicationNames: []string{"test-app"},
		})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
		body := w.Body.String()
		if strings.Contains(body, `application="test-app"`) {
			t.Errorf("expected filtered body not to contain test-app, got:\n%s", body)
		}
	})

	t.Run("MissingApplicationReadPermissionReturns403", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/metrics", nil)
		ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
			Subject:     "key-without-app-read",
			IsAdmin:     false,
			IsAPIKey:    true,
			Permissions: []string{auth.PermWebsocketConnect},
		})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for missing application.read, got %d", w.Code)
		}
	})

	t.Run("OrgScopingIsolatesTenants", func(t *testing.T) {
		orgA := pgtype.UUID{Bytes: [16]byte{0xA}, Valid: true}
		orgB := pgtype.UUID{Bytes: [16]byte{0xB}, Valid: true}
		appA := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
		appB := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

		scopedStore := &mockMetricsStore{
			apps: []gen.Application{
				{ID: appA, Name: "app-org-a", OrganisationID: orgA},
				{ID: appB, Name: "app-org-b", OrganisationID: orgB},
			},
			executors: map[pgtype.UUID][]gen.Executor{
				appA: {{ApplicationID: appA, ExecutorID: "exec-a", Status: "connected", ApplicationVersion: "1.0"}},
				appB: {{ApplicationID: appB, ExecutorID: "exec-b", Status: "connected", ApplicationVersion: "1.0"}},
			},
		}
		scopedHandler := metrics.NewHandler(scopedStore)

		// Scrape with Org A key
		req, _ := http.NewRequest(http.MethodGet, "/v1/metrics", nil)
		ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
			Subject:     "key-org-a",
			OrgID:       orgA,
			IsAdmin:     false,
			IsAPIKey:    true,
			Permissions: []string{auth.PermApplicationRead},
		})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		scopedHandler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, `application="app-org-a"`) {
			t.Errorf("expected body to contain app-org-a, got:\n%s", body)
		}
		if strings.Contains(body, `application="app-org-b"`) {
			t.Errorf("cross-tenant leak: body should not contain app-org-b, got:\n%s", body)
		}
	})

	t.Run("LabelEscapingPreventsMetricForgery", func(t *testing.T) {
		appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
		maliciousVersion := "x\"} 1\nforged_metric{evil=\"yes\"} 99\n#"
		escapedStore := &mockMetricsStore{
			apps: []gen.Application{
				{ID: appID, Name: "app-escape", OrganisationID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}},
			},
			executors: map[pgtype.UUID][]gen.Executor{
				appID: {{ApplicationID: appID, ExecutorID: "exec-1", Status: "connected", ApplicationVersion: maliciousVersion}},
			},
		}
		escHandler := metrics.NewHandler(escapedStore)

		req, _ := http.NewRequest(http.MethodGet, "/v1/metrics", nil)
		ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
			Subject: "admin",
			IsAdmin: true,
		})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		escHandler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}

		body := w.Body.String()
		// Ensure forged_metric does NOT appear as an unescaped new metric line
		lines := strings.Split(body, "\n")
		var metricLines []string
		for _, line := range lines {
			if strings.HasPrefix(line, "dbos_conductor_v1_executor_count{") {
				metricLines = append(metricLines, line)
			}
			if strings.HasPrefix(line, "forged_metric") {
				t.Fatalf("forged metric line found in output: %q", line)
			}
		}
		if len(metricLines) != 1 {
			t.Fatalf("expected exactly 1 metric series, got %d:\n%s", len(metricLines), body)
		}
		if !strings.Contains(body, `application_version="x\"} 1\nforged_metric{evil=\"yes\"} 99\n#"`) {
			t.Errorf("expected escaped label value in output, got:\n%s", body)
		}
	})
}

func TestMetricsEndpoint_OpenMetricsContentNegotiation(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	store := &mockMetricsStore{
		apps: []gen.Application{{ID: appID, Name: "test-app"}},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {{ApplicationID: appID, ExecutorID: "exec-1", Status: "connected", ApplicationVersion: "1.0"}},
		},
	}
	handler := metrics.NewHandler(store)

	t.Run("NegotiatesOpenMetricsWhenRequested", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/metrics", nil)
		req.Header.Set("Accept", "application/openmetrics-text; version=1.0.0, text/plain; version=0.0.4;q=0.5")
		ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
			Subject:  "test",
			IsAdmin:  true,
			IsAPIKey: true,
		})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
		contentType := w.Header().Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/openmetrics-text") {
			t.Errorf("expected Content-Type application/openmetrics-text, got %s", contentType)
		}
		body := w.Body.String()
		if !strings.HasSuffix(strings.TrimSpace(body), "# EOF") {
			t.Errorf("expected body to end with # EOF, got:\n%s", body)
		}
		if count := strings.Count(body, "# TYPE dbos_conductor_v1_"); count != 1 {
			t.Errorf("expected exactly 1 in-scope metric family, got %d in:\n%s", count, body)
		}
	})

	t.Run("DefaultsToTextPlainWithoutOpenMetricsAccept", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/metrics", nil)
		req.Header.Set("Accept", "text/plain")
		ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
			Subject:  "test",
			IsAdmin:  true,
			IsAPIKey: true,
		})
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", w.Code)
		}
		contentType := w.Header().Get("Content-Type")
		if !strings.HasPrefix(contentType, "text/plain") {
			t.Errorf("expected Content-Type text/plain, got %s", contentType)
		}
		body := w.Body.String()
		if strings.Contains(body, "# EOF") {
			t.Errorf("text/plain exposition should not contain # EOF, got:\n%s", body)
		}
	})
}

type mockFailingMetricsStore struct {
	failApps      bool
	failExecutors bool
}

func (m *mockFailingMetricsStore) ListAllApplications(ctx context.Context) ([]gen.Application, error) {
	if m.failApps {
		return nil, errors.New("database error listing apps")
	}
	return []gen.Application{{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Name: "app-1"}}, nil
}

func (m *mockFailingMetricsStore) ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error) {
	if m.failExecutors {
		return nil, errors.New("database error listing executors")
	}
	return nil, nil
}

func (m *mockFailingMetricsStore) GetAPIKeyByLookup(ctx context.Context, lookup string) (gen.ApiKey, error) {
	return gen.ApiKey{}, nil
}

type mockGroupedMetricsStore struct {
	mockMetricsStore
	failGrouped bool
	rows        []metrics.ExecutorCountRow
	callCount   int
}

func (m *mockGroupedMetricsStore) GetExecutorCountsGrouped(ctx context.Context) ([]metrics.ExecutorCountRow, error) {
	m.callCount++
	if m.failGrouped {
		return nil, errors.New("database error grouping executors")
	}
	return m.rows, nil
}

func TestMetricsEndpoint_StoreErrorsReturn500(t *testing.T) {
	adminCtx := func(req *http.Request) *http.Request {
		ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
			Subject:  "test",
			IsAdmin:  true,
			IsAPIKey: true,
		})
		return req.WithContext(ctx)
	}

	t.Run("FailsOnListAllApplications", func(t *testing.T) {
		store := &mockFailingMetricsStore{failApps: true}
		handler := metrics.NewHandler(store)
		req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics", nil))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on ListAllApplications error, got %d", w.Code)
		}
	})

	t.Run("FailsOnListExecutorsByApplication", func(t *testing.T) {
		store := &mockFailingMetricsStore{failExecutors: true}
		handler := metrics.NewHandler(store)
		req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics", nil))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on ListExecutorsByApplication error, got %d", w.Code)
		}
	})

	t.Run("FailsOnGetExecutorCountsGrouped", func(t *testing.T) {
		store := &mockGroupedMetricsStore{failGrouped: true}
		handler := metrics.NewHandler(store)
		req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics", nil))
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500 on GetExecutorCountsGrouped error, got %d", w.Code)
		}
	})
}

func TestMetricsEndpoint_GroupedExecutorStoreOptimization(t *testing.T) {
	store := &mockGroupedMetricsStore{
		rows: []metrics.ExecutorCountRow{
			{ApplicationName: "app-1", ApplicationVersion: "v1.0.0", Status: "connected", Count: 3},
			{ApplicationName: "app-2", ApplicationVersion: "v2.0.0", Status: "disconnected", Count: 1},
		},
	}
	handler := metrics.NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
		Subject:  "test",
		IsAdmin:  true,
		IsAPIKey: true,
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	if store.callCount != 1 {
		t.Errorf("expected exactly 1 grouped store invocation, got %d", store.callCount)
	}

	body := w.Body.String()
	if !strings.Contains(body, `dbos_conductor_v1_executor_count{application="app-1",application_version="v1.0.0",status="HEALTHY"} 3`) {
		t.Errorf("expected app-1 count 3, got:\n%s", body)
	}
	if !strings.Contains(body, `dbos_conductor_v1_executor_count{application="app-2",application_version="v2.0.0",status="DISCONNECTED"} 1`) {
		t.Errorf("expected app-2 count 1, got:\n%s", body)
	}
}

func TestObservabilityAssets_MetricParity(t *testing.T) {
	alertRulesData, err := os.ReadFile("../../deploy/observability/prometheus/relay-alerts.yaml")
	if err != nil {
		t.Fatalf("failed to read relay-alerts.yaml: %v", err)
	}
	dashboardData, err := os.ReadFile("../../deploy/observability/dashboards/relay-overview.json")
	if err != nil {
		t.Fatalf("failed to read relay-overview.json: %v", err)
	}

	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	store := &mockMetricsStore{
		apps: []gen.Application{{ID: appID, Name: "test-app"}},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {{ApplicationID: appID, ExecutorID: "exec-1", Status: "connected", ApplicationVersion: "1.0"}},
		},
	}
	handler := metrics.NewHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{Subject: "admin", IsAdmin: true})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("metrics endpoint returned %d: %s", w.Code, w.Body.String())
	}
	emittedBody := w.Body.String()

	extractMetricNames := func(content string) []string {
		exprRegex := regexp.MustCompile(`expr:\s*([^\n]+)|"expr":\s*"([^"]+)"`)
		identRegex := regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_]*`)
		matches := exprRegex.FindAllStringSubmatch(content, -1)
		names := make(map[string]struct{})
		promqlKeywords := map[string]bool{
			"sum": true, "count": true, "avg": true, "min": true, "max": true,
			"by": true, "without": true, "rate": true, "irate": true,
			"increase": true, "status": true, "application": true,
			"application_version": true, "HEALTHY": true, "DISCONNECTED": true, "DEAD": true,
		}
		for _, m := range matches {
			expr := m[1]
			if expr == "" {
				expr = m[2]
			}
			tokens := identRegex.FindAllString(expr, -1)
			for _, token := range tokens {
				if !promqlKeywords[token] {
					names[token] = struct{}{}
				}
			}
		}
		var result []string
		for name := range names {
			result = append(result, name)
		}
		return result
	}

	referencedInAlerts := extractMetricNames(string(alertRulesData))
	referencedInDashboard := extractMetricNames(string(dashboardData))

	if len(referencedInAlerts) == 0 {
		t.Fatal("expected at least one metric name extracted from relay-alerts.yaml")
	}
	if len(referencedInDashboard) == 0 {
		t.Fatal("expected at least one metric name extracted from relay-overview.json")
	}

	allReferenced := append(referencedInAlerts, referencedInDashboard...)
	for _, metricName := range allReferenced {
		expectedHeader := "# TYPE " + metricName
		if !strings.Contains(emittedBody, expectedHeader) {
			t.Errorf("metric %q referenced in observability assets is not emitted by metrics endpoint", metricName)
		}
	}
}
