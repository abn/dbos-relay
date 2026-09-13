package hub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/store/gen"
)

type memoryAuthStore struct {
	keys map[string]gen.ApiKey
	apps map[string]gen.Application
	orgs map[string]gen.Organisation
}

func newMemoryAuthStore() *memoryAuthStore {
	return &memoryAuthStore{
		keys: make(map[string]gen.ApiKey),
		apps: make(map[string]gen.Application),
		orgs: make(map[string]gen.Organisation),
	}
}

func (m *memoryAuthStore) GetAPIKeyByLookup(ctx context.Context, lookup string) (gen.ApiKey, error) {
	key, ok := m.keys[lookup]
	if !ok {
		return gen.ApiKey{}, pgx.ErrNoRows
	}
	return key, nil
}

func (m *memoryAuthStore) GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
	app, ok := m.apps[arg.Name]
	if !ok {
		return gen.Application{}, pgx.ErrNoRows
	}
	return app, nil
}

func (m *memoryAuthStore) CreateApplication(ctx context.Context, arg gen.CreateApplicationParams) (gen.Application, error) {
	app := gen.Application{
		ID:             pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true},
		OrganisationID: arg.OrganisationID,
		Name:           arg.Name,
	}
	m.apps[arg.Name] = app
	return app, nil
}

func (m *memoryAuthStore) TouchAPIKeyLastUsed(ctx context.Context, id pgtype.UUID) error {
	return nil
}

func (m *memoryAuthStore) GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error) {
	org, ok := m.orgs[name]
	if !ok {
		return gen.Organisation{}, pgx.ErrNoRows
	}
	return org, nil
}

func (m *memoryAuthStore) UpsertOrganisation(ctx context.Context, name string) (gen.Organisation, error) {
	org := gen.Organisation{
		ID:   pgtype.UUID{Bytes: [16]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}, Valid: true},
		Name: name,
	}
	m.orgs[name] = org
	return org, nil
}

func TestAuthenticate_ScopeAndValidation(t *testing.T) {
	ctx := context.Background()

	// Mint a scoped key
	plainScoped, recScoped, err := auth.Mint()
	if err != nil {
		t.Fatalf("auth.Mint failed: %v", err)
	}

	// Mint an unscoped key
	plainUnscoped, recUnscoped, err := auth.Mint()
	if err != nil {
		t.Fatalf("auth.Mint failed: %v", err)
	}

	store := newMemoryAuthStore()
	orgID := pgtype.UUID{Bytes: [16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, Valid: true}

	store.keys[recScoped.Lookup] = gen.ApiKey{
		ID:               pgtype.UUID{Bytes: [16]byte{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}, Valid: true},
		OrganisationID:   orgID,
		Lookup:           recScoped.Lookup,
		KeyHash:          recScoped.Hash,
		ApplicationNames: []string{"app-a"},
	}

	store.keys[recUnscoped.Lookup] = gen.ApiKey{
		ID:               pgtype.UUID{Bytes: [16]byte{3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3}, Valid: true},
		OrganisationID:   orgID,
		Lookup:           recUnscoped.Lookup,
		KeyHash:          recUnscoped.Hash,
		ApplicationNames: []string{},
	}

	tests := []struct {
		name        string
		appName     string
		key         string
		authEnabled bool
		wantErr     bool
		errContains string
	}{
		{
			name:        "scoped key matching app succeeds",
			appName:     "app-a",
			key:         plainScoped,
			authEnabled: true,
			wantErr:     false,
		},
		{
			name:        "scoped key mismatching app returns 403 scope error",
			appName:     "app-b",
			key:         plainScoped,
			authEnabled: true,
			wantErr:     true,
			errContains: "key does not have access to this application",
		},
		{
			name:        "unscoped key succeeds for app-a",
			appName:     "app-a",
			key:         plainUnscoped,
			authEnabled: true,
			wantErr:     false,
		},
		{
			name:        "unscoped key succeeds for app-b",
			appName:     "app-b",
			key:         plainUnscoped,
			authEnabled: true,
			wantErr:     false,
		},
		{
			name:        "valid lookup with wrong secret returns invalid key",
			appName:     "app-a",
			key:         recScoped.Lookup + "_wrongsecretwrongsecret12345",
			authEnabled: true,
			wantErr:     true,
			errContains: "invalid conductor key",
		},
		{
			name:        "unknown key lookup returns invalid key",
			appName:     "app-a",
			key:         "dbos_m_unknownkeyunknownkey12345",
			authEnabled: true,
			wantErr:     true,
			errContains: "invalid conductor key",
		},
		{
			name:        "empty app name rejected",
			appName:     "",
			key:         plainScoped,
			authEnabled: true,
			wantErr:     true,
			errContains: "missing app name or conductor key",
		},
		{
			name:        "empty key rejected",
			appName:     "app-a",
			key:         "",
			authEnabled: true,
			wantErr:     true,
			errContains: "missing app name or conductor key",
		},
		{
			name:        "unknown key in no-auth mode rejected",
			appName:     "any-app",
			key:         "any-key",
			authEnabled: false,
			wantErr:     true,
			errContains: "invalid conductor key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			appID, err := Authenticate(ctx, store, tc.appName, tc.key, tc.authEnabled)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error containing %q, got %q", tc.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !appID.Valid {
					t.Fatalf("expected valid appID, got invalid")
				}
			}
		})
	}
}

func TestHandleAuthError_Branches(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedDetail string
	}{
		{
			name:           "scope denial maps to 403 Forbidden",
			err:            errors.New("key does not have access to this application"),
			expectedStatus: http.StatusForbidden,
			expectedDetail: "key does not have access to this application",
		},
		{
			name:           "invalid key maps to 401 Unauthorized",
			err:            errors.New("invalid conductor key"),
			expectedStatus: http.StatusUnauthorized,
			expectedDetail: "invalid conductor key",
		},
		{
			name:           "missing key maps to 401 Unauthorized",
			err:            errors.New("missing app name or conductor key"),
			expectedStatus: http.StatusUnauthorized,
			expectedDetail: "missing app name or conductor key",
		},
		{
			name:           "arbitrary error maps to 500 Internal Server Error",
			err:            errors.New("database connection failure"),
			expectedStatus: http.StatusInternalServerError,
			expectedDetail: "an error occurred during authentication",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/websocket/app/key", nil)
			HandleAuthError(rec, req, tc.err)

			if rec.Code != tc.expectedStatus {
				t.Fatalf("expected status %d, got %d", tc.expectedStatus, rec.Code)
			}
			contentType := rec.Header().Get("Content-Type")
			if !strings.Contains(contentType, "application/problem+json") {
				t.Fatalf("expected Content-Type application/problem+json, got %q", contentType)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.expectedDetail) {
				t.Fatalf("expected body to contain %q, got %q", tc.expectedDetail, body)
			}
		})
	}
}

func TestWebSocket_ScopeDenialEndToEnd(t *testing.T) {
	plainScoped, recScoped, err := auth.Mint()
	if err != nil {
		t.Fatalf("auth.Mint failed: %v", err)
	}

	store := newMemoryAuthStore()
	orgID := pgtype.UUID{Bytes: [16]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, Valid: true}
	store.keys[recScoped.Lookup] = gen.ApiKey{
		ID:               pgtype.UUID{Bytes: [16]byte{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2}, Valid: true},
		OrganisationID:   orgID,
		Lookup:           recScoped.Lookup,
		KeyHash:          recScoped.Hash,
		ApplicationNames: []string{"app-allowed"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/websocket/"), "/")
		if len(parts) < 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		appName, key := parts[0], parts[1]
		_, authErr := Authenticate(r.Context(), store, appName, key, true)
		if authErr != nil {
			HandleAuthError(w, r, authErr)
			return
		}
		// Accept upgrade if authenticated
		c, upgradeErr := websocket.Accept(w, r, nil)
		if upgradeErr != nil {
			return
		}
		_ = c.Close(websocket.StatusNormalClosure, "ok")
	}))
	defer server.Close()

	wsBase := "ws" + strings.TrimPrefix(server.URL, "http")

	// Case 1: Dial disallowed app -> 403 Forbidden
	ctx := context.Background()
	badURL := wsBase + "/websocket/app-disallowed/" + plainScoped
	conn, resp, dialErr := websocket.Dial(ctx, badURL, nil)
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		t.Fatalf("expected dial to fail, but got active conn")
	}
	if resp == nil {
		t.Fatalf("expected HTTP response on rejected upgrade, got %v", dialErr)
	}
	if resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403 Forbidden, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/problem+json") {
		t.Fatalf("expected Content-Type application/problem+json, got %q", ct)
	}

	// Case 2: Dial allowed app -> succeeds
	goodURL := wsBase + "/websocket/app-allowed/" + plainScoped
	connGood, respGood, errGood := websocket.Dial(ctx, goodURL, nil)
	if errGood != nil {
		t.Fatalf("expected dial to succeed for allowed app, got %v", errGood)
	}
	if respGood != nil && respGood.Body != nil {
		_ = respGood.Body.Close()
	}
	if connGood != nil {
		_ = connGood.Close(websocket.StatusNormalClosure, "done")
	}
}
