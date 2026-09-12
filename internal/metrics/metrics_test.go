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
}
