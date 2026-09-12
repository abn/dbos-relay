package identity_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/abn/relay/internal/auth"
)

func TestRouteAuthScopes(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()
	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// Create orgs
	_, _ = store.UpsertOrganisation(context.Background(), "alpha")
	_, _ = store.UpsertOrganisation(context.Background(), "beta")

	// Mint token for user in alpha
	token := idp.mintToken(t, map[string]any{
		"sub":                "user-alpha",
		"email":              "user@alpha.corp",
		"preferred_username": "user-alpha",
		"iss":                idp.server.URL,
		"aud":                "relay-client",
		"exp":                time.Now().Add(time.Hour).Unix(),
	})

	// Do one request to alpha to auto-register
	req, _ := http.NewRequest("GET", ts.URL+"/v2/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := http.DefaultClient.Do(req)
	_ = resp.Body.Close()

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/v2/orgs/beta/apps"},
		{"POST", "/v2/orgs/beta/apps/app1"},
		{"DELETE", "/v2/orgs/beta/apps/app1"},
		{"GET", "/v2/orgs/beta/members"},
		{"GET", "/v2/orgs/beta/roles"},
		{"POST", "/v2/orgs/beta/roles"},
		{"DELETE", "/v2/orgs/beta/roles/auditor"},
		{"GET", "/v2/orgs/beta/tokens"},
		{"POST", "/v2/orgs/beta/tokens/key1"},
		{"DELETE", "/v2/orgs/beta/tokens/key1"},
	}

	for _, route := range routes {
		t.Run(route.method+"_"+route.path, func(t *testing.T) {
			req, _ := http.NewRequest(route.method, ts.URL+route.path, bytes.NewReader([]byte("{}")))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				t.Errorf("expected 401 or 403, got %d", resp.StatusCode)
			}
		})
	}
}
