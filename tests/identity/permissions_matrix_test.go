package identity_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abn/relay/internal/auth"
	storegen "github.com/abn/relay/internal/store/gen"
)

func operatorToken(t *testing.T, idp *mockIdP, sub string) string {
	t.Helper()
	return idp.mintToken(t, map[string]any{
		"sub": sub,
		"iss": idp.server.URL,
		"aud": "relay-client",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
}

func doRequest(t *testing.T, ts *httptest.Server, method, path, token, body string) int {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// An operator holds organization.write under the current role defaults,
// so organization mutations must succeed for operators and fail for
// viewers. This pins the requireOrgWrite cutover: the previous isAdmin
// role check denied operators here.
func TestIdentity_OperatorWithOrgWriteCanMutate(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()
	ctx := context.Background()

	org, _ := store.UpsertOrganisation(ctx, "acme")
	operator, _ := store.UpsertUser(ctx, storegen.UpsertUserParams{
		Subject: "operator-sub", Username: "operator-sub", Email: "op@acme.corp",
	})
	viewer, _ := store.UpsertUser(ctx, storegen.UpsertUserParams{
		Subject: "viewer-sub", Username: "viewer-sub", Email: "viewer@acme.corp",
	})
	if _, err := store.UpsertMemberRole(ctx, storegen.UpsertMemberRoleParams{
		OrganisationID: org.ID, UserID: operator.ID, RoleName: auth.RoleOperator,
	}); err != nil {
		t.Fatalf("grant operator: %v", err)
	}
	if _, err := store.UpsertMemberRole(ctx, storegen.UpsertMemberRoleParams{
		OrganisationID: org.ID, UserID: viewer.ID, RoleName: auth.RoleViewer,
	}); err != nil {
		t.Fatalf("grant viewer: %v", err)
	}

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	opToken := operatorToken(t, idp, "operator-sub")
	viewerToken := operatorToken(t, idp, "viewer-sub")

	if got := doRequest(t, ts, "PATCH", "/v2/orgs/acme", opToken, `{"auditLogRetentionDays":30}`); got != http.StatusNoContent {
		t.Errorf("operator PATCH org = %d, want 204", got)
	}
	if got := doRequest(t, ts, "POST", "/v2/orgs/acme/roles", opToken, `{"name":"auditor"}`); got != http.StatusCreated {
		t.Errorf("operator POST role = %d, want 201", got)
	}
	if got := doRequest(t, ts, "PATCH", "/v2/orgs/acme", viewerToken, `{"auditLogRetentionDays":30}`); got != http.StatusForbidden {
		t.Errorf("viewer PATCH org = %d, want 403", got)
	}
	if got := doRequest(t, ts, "POST", "/v2/orgs/acme/roles", viewerToken, `{"name":"auditor"}`); got != http.StatusForbidden {
		t.Errorf("viewer POST role = %d, want 403", got)
	}
}

func mintKey(t *testing.T, store *memoryStore, org storegen.Organisation, name string, perms []string) string {
	t.Helper()
	plain, rec, err := auth.Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := store.CreateAPIKey(context.Background(), storegen.CreateAPIKeyParams{
		OrganisationID: org.ID, Name: name, Lookup: rec.Lookup, KeyHash: rec.Hash,
		Permissions: perms,
	}); err != nil {
		t.Fatalf("create key: %v", err)
	}
	return plain
}

// API keys exercise route-level permissions end to end through the real
// middleware chain: token routes need token.read/write, per-app metrics
// need metric.read, the app registry keeps application.read, the
// permissions catalog stays open, and org resources need organization.read.
func TestIdentity_APIKeyPermissionMatrix(t *testing.T) {
	store := newMemoryStore()
	ctx := context.Background()
	org, _ := store.UpsertOrganisation(ctx, "acme")

	appReader := mintKey(t, store, org, "app-reader", []string{auth.PermApplicationRead})
	tokenReader := mintKey(t, store, org, "token-reader", []string{auth.PermTokenRead})
	tokenWriter := mintKey(t, store, org, "token-writer", []string{auth.PermTokenRead, auth.PermTokenWrite})
	metricReader := mintKey(t, store, org, "metric-reader", []string{auth.PermMetricRead})
	orgReader := mintKey(t, store, org, "org-reader", []string{auth.PermApplicationRead, auth.PermOrgRead})
	orgWriter := mintKey(t, store, org, "org-writer", []string{auth.PermOrgRead, auth.PermOrgWrite})

	ts := setupServer(store, true, nil)
	defer ts.Close()

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		body   string
		want   int
	}{
		{"app registry readable with application.read", "GET", "/v2/orgs/acme/apps", appReader, "", 200},
		{"token list denied without token.read", "GET", "/v2/orgs/acme/tokens", appReader, "", 403},
		{"token list allowed with token.read", "GET", "/v2/orgs/acme/tokens", tokenReader, "", 200},
		{"token mint denied without token.write", "POST", "/v2/orgs/acme/tokens/new-key", tokenReader, `{"permissions":["token.read"]}`, 403},
		{"token mint allowed with token.write", "POST", "/v2/orgs/acme/tokens/new-key", tokenWriter, `{"permissions":["token.read"]}`, 201},
		{"token revoke allowed with token.write", "DELETE", "/v2/orgs/acme/tokens/new-key", tokenWriter, "", 204},
		{"token revoke denied without token.write", "DELETE", "/v2/orgs/acme/tokens/new-key", tokenReader, "", 403},
		{"permissions catalog open to any key", "GET", "/v2/orgs/acme/permissions", appReader, "", 200},
		{"members denied without organization.read", "GET", "/v2/orgs/acme/members", appReader, "", 403},
		{"members allowed with organization.read", "GET", "/v2/orgs/acme/members", orgReader, "", 200},
		{"org read denied without organization.read", "GET", "/v2/orgs/acme", appReader, "", 403},
		{"org read allowed with organization.read", "GET", "/v2/orgs/acme", orgReader, "", 200},
		{"org update denied without organization.write", "PATCH", "/v2/orgs/acme", orgReader, `{"auditLogRetentionDays":30}`, 403},
		{"org update allowed with organization.write", "PATCH", "/v2/orgs/acme", orgWriter, `{"auditLogRetentionDays":30}`, 204},
		{"unauthenticated rejected", "GET", "/v2/orgs/acme/apps", "", "", 401},
	}
	for _, c := range cases {
		if got := doRequest(t, ts, c.method, c.path, c.token, c.body); got != c.want {
			t.Errorf("%s: %s %s = %d, want %d", c.name, c.method, c.path, got, c.want)
		}
	}

	// Per-app metrics accept metric.read or application.read, matching
	// the scrape endpoint. Without a live executor the dispatch itself
	// fails with 500 (nil response from the test double), which still
	// proves the denial did not fire.
	metricsURL := "/v2/orgs/acme/apps/shop/metrics?startTime=2026-01-01T00:00:00Z&endTime=2026-01-02T00:00:00Z"
	for name, token := range map[string]string{"metric.read": metricReader, "application.read": appReader} {
		if got := doRequest(t, ts, "GET", metricsURL, token, ""); got != http.StatusInternalServerError {
			t.Errorf("per-app metrics with %s = %d, want 500 (middleware passed, no executor)", name, got)
		}
	}
}
