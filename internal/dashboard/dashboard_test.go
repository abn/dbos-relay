package dashboard_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abn/relay/internal/dashboard"
)

func TestRootServesIndexHtml(t *testing.T) {
	mockAPI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("api response"))
	})

	handler := dashboard.Handler(mockAPI)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %s", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Relay Dashboard") {
		t.Errorf("expected body to contain 'Relay Dashboard', got %s", body)
	}
	if !strings.Contains(body, `<div id="app">`) {
		t.Errorf("expected body to contain '<div id=\"app\">', got %s", body)
	}
}

func TestStaticAssetsServedWithMimeType(t *testing.T) {
	handler := dashboard.Handler(nil)

	// JS Asset
	reqJS := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	wJS := httptest.NewRecorder()
	handler.ServeHTTP(wJS, reqJS)

	if wJS.Code != http.StatusOK {
		t.Fatalf("expected /assets/app.js status 200, got %d", wJS.Code)
	}
	ctJS := wJS.Header().Get("Content-Type")
	if !strings.Contains(ctJS, "javascript") {
		t.Errorf("expected javascript MIME type, got %s", ctJS)
	}

	// CSS Asset
	reqCSS := httptest.NewRequest(http.MethodGet, "/assets/app.css", nil)
	wCSS := httptest.NewRecorder()
	handler.ServeHTTP(wCSS, reqCSS)

	if wCSS.Code != http.StatusOK {
		t.Fatalf("expected /assets/app.css status 200, got %d", wCSS.Code)
	}
	ctCSS := wCSS.Header().Get("Content-Type")
	if !strings.Contains(ctCSS, "text/css") {
		t.Errorf("expected text/css MIME type, got %s", ctCSS)
	}
}

func TestMissingAssetReturns404(t *testing.T) {
	handler := dashboard.Handler(nil)

	req := httptest.NewRequest(http.MethodGet, "/assets/nonexistent.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for missing asset, got %d", w.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	handler := dashboard.Handler(nil)

	subroutes := []string{
		"/workflows",
		"/workflows/wf-999",
		"/fleet",
		"/queues",
		"/schedules",
		"/apps/order-service",
	}

	for _, path := range subroutes {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200 for %s, got %d", path, w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected text/html for %s, got %s", path, ct)
		}
		if !strings.Contains(w.Body.String(), "Relay Dashboard") {
			t.Errorf("expected %s to serve index.html", path)
		}
	}
}

func TestAPIRoutesPassThrough(t *testing.T) {
	mockAPI := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"api":"ok"}`))
	})

	handler := dashboard.Handler(mockAPI)

	apiPaths := []string{
		"/v2/orgs/default/apps",
		"/v1/metrics",
		"/healthz",
		"/openapi.json",
		"/docs",
		"/websocket/app/key",
		"/internal/v1/forward/123",
	}

	for _, path := range apiPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected API route %s status 200, got %d", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), `{"api":"ok"}`) {
			t.Errorf("expected API route %s to be handled by apiHandler, got %s", path, w.Body.String())
		}
	}
}
