package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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
