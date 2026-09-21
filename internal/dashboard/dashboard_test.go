package dashboard_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/abn/relay/internal/dashboard"
)

// fingerprintedAsset fetches the served index and returns the asset URL it
// references (assets/app.<12 hex hash>.<ext>). Tests resolve bundle URLs
// through the served page so a stale reference fails loudly.
func fingerprintedAsset(t *testing.T, handler http.Handler, ext string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", w.Code)
	}
	re := regexp.MustCompile(`assets/app\.([0-9a-f]{12})\.` + ext)
	m := re.FindString(w.Body.String())
	if m == "" {
		t.Fatalf("served index references no fingerprinted .%s bundle", ext)
	}
	return "/" + m
}

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
	jsPath := fingerprintedAsset(t, handler, "js")
	cssPath := fingerprintedAsset(t, handler, "css")

	// JS Asset
	reqJS := httptest.NewRequest(http.MethodGet, jsPath, nil)
	wJS := httptest.NewRecorder()
	handler.ServeHTTP(wJS, reqJS)

	if wJS.Code != http.StatusOK {
		t.Fatalf("expected %s status 200, got %d", jsPath, wJS.Code)
	}
	ctJS := wJS.Header().Get("Content-Type")
	if !strings.Contains(ctJS, "javascript") {
		t.Errorf("expected javascript MIME type, got %s", ctJS)
	}

	// CSS Asset
	reqCSS := httptest.NewRequest(http.MethodGet, cssPath, nil)
	wCSS := httptest.NewRecorder()
	handler.ServeHTTP(wCSS, reqCSS)

	if wCSS.Code != http.StatusOK {
		t.Fatalf("expected %s status 200, got %d", cssPath, wCSS.Code)
	}
	ctCSS := wCSS.Header().Get("Content-Type")
	if !strings.Contains(ctCSS, "text/css") {
		t.Errorf("expected text/css MIME type, got %s", ctCSS)
	}
}

func TestFingerprintedAssetsAreImmutable(t *testing.T) {
	handler := dashboard.Handler(nil)
	for _, ext := range []string{"js", "css"} {
		assetPath := fingerprintedAsset(t, handler, ext)
		req := httptest.NewRequest(http.MethodGet, assetPath, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected %s status 200, got %d", assetPath, w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Errorf("expected immutable Cache-Control for %s, got %q", assetPath, cc)
		}
	}

	// The legacy unhashed bundle names must not resolve: keeping them
	// servable would reintroduce the stale-UI path.
	for _, legacy := range []string{"/assets/app.js", "/assets/app.css"} {
		req := httptest.NewRequest(http.MethodGet, legacy, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for legacy bundle %s, got %d", legacy, w.Code)
		}
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

func TestDashboard_SecurityHeaders(t *testing.T) {
	handler := dashboard.Handler(nil)
	jsPath := fingerprintedAsset(t, handler, "js")

	testCases := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "root index", path: "/", wantStatus: http.StatusOK},
		{name: "static asset", path: jsPath, wantStatus: http.StatusOK},
		{name: "spa fallback", path: "/workflows", wantStatus: http.StatusOK},
		{name: "missing asset with ext", path: "/assets/nonexistent.js", wantStatus: http.StatusNotFound},
		{name: "missing image asset", path: "/nope.png", wantStatus: http.StatusNotFound},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("expected status %d for %s, got %d", tc.wantStatus, tc.path, w.Code)
			}

			if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("expected X-Content-Type-Options: nosniff for %s, got %q", tc.path, got)
			}
			if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("expected X-Frame-Options: DENY for %s, got %q", tc.path, got)
			}
			if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("expected Referrer-Policy: no-referrer for %s, got %q", tc.path, got)
			}
			csp := w.Header().Get("Content-Security-Policy")
			if !strings.Contains(csp, "script-src 'self'") {
				t.Errorf("expected CSP to contain script-src 'self' for %s, got %q", tc.path, csp)
			}
			if !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
				t.Errorf("expected CSP to contain style-src 'self' 'unsafe-inline' for %s, got %q", tc.path, csp)
			}
			if !strings.Contains(csp, "frame-ancestors 'none'") {
				t.Errorf("expected CSP to contain frame-ancestors 'none' for %s, got %q", tc.path, csp)
			}
		})
	}
}
