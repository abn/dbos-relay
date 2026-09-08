package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/problem"
)

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(_ context.Context) error {
	return m.err
}

func TestHealthzReportsDatabaseState(t *testing.T) {
	t.Run("healthy database", func(t *testing.T) {
		h := api.NewHandler(&mockPinger{err: nil})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

		h.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d", got, want)
		}
		if got, want := rec.Header().Get("Content-Type"), "application/json"; got != want {
			t.Errorf("Content-Type = %q, want %q", got, want)
		}
		if got, want := strings.TrimSpace(rec.Body.String()), `{"status":"ok"}`; got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
	})

	t.Run("unreachable database", func(t *testing.T) {
		h := api.NewHandler(&mockPinger{err: errors.New("connection refused")})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

		h.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
			t.Fatalf("status = %d, want %d", got, want)
		}
		if got, want := rec.Header().Get("Content-Type"), "application/problem+json"; got != want {
			t.Errorf("Content-Type = %q, want %q", got, want)
		}

		var prob problem.Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
			t.Fatalf("unmarshal problem: %v", err)
		}
		if prob.Status != http.StatusServiceUnavailable {
			t.Errorf("problem status = %d, want %d", prob.Status, http.StatusServiceUnavailable)
		}
		if prob.Title != "database unavailable" {
			t.Errorf("problem title = %q, want \"database unavailable\"", prob.Title)
		}
		if prob.Detail != "connection refused" {
			t.Errorf("problem detail = %q, want \"connection refused\"", prob.Detail)
		}
	})
}

func TestSpecIsServedAtEveryDocumentedPath(t *testing.T) {
	h := api.NewHandler(&mockPinger{})

	paths := []string{"/openapi.json", "/openapi.yaml", "/openapi-3.0.json"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)

			h.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d", got, want)
			}
			if rec.Body.Len() == 0 {
				t.Fatalf("expected non-empty body for %s", path)
			}

			switch path {
			case "/openapi.json", "/openapi-3.0.json":
				contentType := rec.Header().Get("Content-Type")
				if !strings.Contains(contentType, "application/json") {
					t.Errorf("Content-Type = %q, want application/json", contentType)
				}
				var doc map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
					t.Fatalf("unmarshal json: %v", err)
				}
				if doc["openapi"] == nil || doc["openapi"] == "" {
					t.Errorf("missing openapi field in %s", path)
				}
			case "/openapi.yaml":
				contentType := rec.Header().Get("Content-Type")
				if !strings.Contains(contentType, "yaml") {
					t.Errorf("Content-Type = %q, want yaml media type", contentType)
				}
				body := rec.Body.String()
				if !strings.Contains(body, "openapi:") {
					t.Errorf("yaml body missing openapi field")
				}
			}
		})
	}
}

func TestSpecPathsNeedNoCredentials(t *testing.T) {
	h := api.NewHandler(&mockPinger{})

	paths := []string{"/openapi.json", "/healthz"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)

			if auth := req.Header.Get("Authorization"); auth != "" {
				t.Fatalf("unexpected authorization header: %s", auth)
			}

			h.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d", got, want)
			}
		})
	}
}

func TestDocsEndpoint(t *testing.T) {
	h := api.NewHandler(&mockPinger{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)

	h.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", contentType)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty html body")
	}
	if !strings.Contains(rec.Body.String(), "/openapi.json") {
		t.Errorf("docs page does not reference /openapi.json")
	}
}

func TestSchemasEndpoint(t *testing.T) {
	h := api.NewHandler(&mockPinger{})

	t.Run("specific schema", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/schemas/AlertingRule.json", nil)

		h.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d", got, want)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
		}
		var schema map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &schema); err != nil {
			t.Fatalf("unmarshal schema json: %v", err)
		}
		if schema["type"] != "object" {
			t.Errorf("schema type = %v, want object", schema["type"])
		}
	})

	t.Run("schemas index redirects to openapi components", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/schemas/", nil)

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound && rec.Code != http.StatusMovedPermanently {
			t.Fatalf("status = %d, want redirect", rec.Code)
		}
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "/openapi.json#/components/schemas") {
			t.Errorf("redirect location = %q, want openapi components fragment", loc)
		}
	})
}
