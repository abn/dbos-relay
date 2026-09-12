package identity_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api"
	apigen "github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/protocol"
	storegen "github.com/abn/relay/internal/store/gen"
)

// mockIdP implements an RFC 8628 Device Authorization and OpenID Connect Discovery provider.
type mockIdP struct {
	privKey  *rsa.PrivateKey
	kid      string
	server   *httptest.Server
	failJWKS bool
	mu       sync.Mutex
}

func newMockIdP(t *testing.T) *mockIdP {
	t.Helper()
	t.Logf("identity provider: in-test mockIdP stand-in (not a real external OIDC provider)")
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	kid := "test-idp-key"
	m := &mockIdP{privKey: priv, kid: kid}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                        base,
			"jwks_uri":                      base + "/jwks.json",
			"device_authorization_endpoint": base + "/oauth/device/code",
			"token_endpoint":                base + "/oauth/token",
		})
	})

	mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		fail := m.failJWKS
		m.mu.Unlock()

		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"jwks unavailable"}`))
			return
		}

		nStr := base64.RawURLEncoding.EncodeToString(priv.N.Bytes())
		eBytes := big.NewInt(int64(priv.E)).Bytes()
		eStr := base64.RawURLEncoding.EncodeToString(eBytes)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"kid": kid,
					"n":   nStr,
					"e":   eStr,
					"alg": "RS256",
					"use": "sig",
				},
			},
		})
	})

	// RFC 8628 Device Authorization Endpoint
	mux.HandleFunc("/oauth/device/code", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "mock-device-code-12345",
			"user_code":        "ABCD-WXYZ",
			"verification_uri": base + "/activate",
			"expires_in":       300,
			"interval":         1,
		})
	})

	// OAuth Token Endpoint
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		token := m.mintToken(t, map[string]any{
			"sub":                "alice-sub-id",
			"email":              "alice@example.com",
			"preferred_username": "alice",
			"iss":                base,
			"aud":                "relay-client",
			"exp":                time.Now().Add(time.Hour).Unix(),
		})

		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"id_token":     token,
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})

	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockIdP) setFailJWKS(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failJWKS = fail
}

func (m *mockIdP) mintToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]string{
		"alg": "RS256",
		"kid": m.kid,
		"typ": "JWT",
	}
	hBytes, _ := json.Marshal(header)
	pBytes, _ := json.Marshal(claims)

	hPart := base64.RawURLEncoding.EncodeToString(hBytes)
	pPart := base64.RawURLEncoding.EncodeToString(pBytes)
	content := hPart + "." + pPart

	hashed := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, m.privKey, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15: %v", err)
	}
	sPart := base64.RawURLEncoding.EncodeToString(sig)

	return content + "." + sPart
}

type memoryStore struct {
	mu           sync.Mutex
	orgs         map[string]storegen.Organisation
	orgsByID     map[pgtype.UUID]storegen.Organisation
	apps         map[string]storegen.Application
	keys         map[string]storegen.ApiKey
	users        map[string]storegen.User
	usersByID    map[pgtype.UUID]storegen.User
	members      map[string]storegen.OrganisationMember
	roles        map[string]storegen.Role
	domainClaims map[string]storegen.DomainClaim
	auditLogs    []storegen.AuditLog
	touchedKeys  map[pgtype.UUID]time.Time
	idCounter    byte
}

func newMemoryStore() *memoryStore {
	m := &memoryStore{
		orgs:         make(map[string]storegen.Organisation),
		orgsByID:     make(map[pgtype.UUID]storegen.Organisation),
		apps:         make(map[string]storegen.Application),
		keys:         make(map[string]storegen.ApiKey),
		users:        make(map[string]storegen.User),
		usersByID:    make(map[pgtype.UUID]storegen.User),
		members:      make(map[string]storegen.OrganisationMember),
		roles:        make(map[string]storegen.Role),
		domainClaims: make(map[string]storegen.DomainClaim),
		auditLogs:    make([]storegen.AuditLog, 0),
		touchedKeys:  make(map[pgtype.UUID]time.Time),
		idCounter:    1,
	}
	// Seed global roles
	for _, rName := range []string{auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer} {
		var perms []string
		switch rName {
		case auth.RoleAdmin, auth.RoleOperator:
			perms = []string{auth.PermApplicationRead, auth.PermApplicationWrite, auth.PermWebsocketConnect}
		case auth.RoleViewer:
			perms = []string{auth.PermApplicationRead}
		}
		m.roles["global:"+rName] = storegen.Role{
			ID:          pgtype.UUID{Bytes: [16]byte{0, 0, 0, byte(len(m.roles) + 1)}, Valid: true},
			Name:        rName,
			Permissions: perms,
		}
	}
	return m
}

func (m *memoryStore) nextUUID() pgtype.UUID {
	m.idCounter++
	return pgtype.UUID{Bytes: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, m.idCounter}, Valid: true}
}

func (m *memoryStore) Ping(_ context.Context) error { return nil }

func (m *memoryStore) GetOrganisationByName(_ context.Context, name string) (storegen.Organisation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if org, ok := m.orgs[name]; ok {
		return org, nil
	}
	return storegen.Organisation{}, fmt.Errorf("org not found: %s: %w", name, pgx.ErrNoRows)
}

func (m *memoryStore) GetOrganisationByID(_ context.Context, id pgtype.UUID) (storegen.Organisation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if org, ok := m.orgsByID[id]; ok {
		return org, nil
	}
	return storegen.Organisation{}, fmt.Errorf("org not found by id: %v", id)
}

func (m *memoryStore) UpsertOrganisation(_ context.Context, name string) (storegen.Organisation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if org, ok := m.orgs[name]; ok {
		return org, nil
	}
	org := storegen.Organisation{
		ID:        m.nextUUID(),
		Name:      name,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	m.orgs[name] = org
	m.orgsByID[org.ID] = org
	return org, nil
}

func (m *memoryStore) GetApplicationByName(_ context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if app, ok := m.apps[arg.Name]; ok {
		return app, nil
	}
	return storegen.Application{}, fmt.Errorf("app not found: %s", arg.Name)
}

func (m *memoryStore) ListApplicationsByOrganisation(_ context.Context, orgID pgtype.UUID) ([]storegen.Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []storegen.Application
	for _, app := range m.apps {
		if app.OrganisationID == orgID {
			res = append(res, app)
		}
	}
	return res, nil
}

func (m *memoryStore) UpsertApplication(_ context.Context, arg storegen.UpsertApplicationParams) (storegen.Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app := storegen.Application{
		ID:             m.nextUUID(),
		OrganisationID: arg.OrganisationID,
		Name:           arg.Name,
		Settings:       arg.Settings,
		CreatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	m.apps[arg.Name] = app
	return app, nil
}

func (m *memoryStore) UpdateApplicationSettings(_ context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app, ok := m.apps[arg.Name]
	if !ok {
		return storegen.Application{}, fmt.Errorf("app not found: %s", arg.Name)
	}
	app.Settings = arg.Settings
	m.apps[arg.Name] = app
	return app, nil
}

func (m *memoryStore) DeleteApplication(_ context.Context, arg storegen.DeleteApplicationParams) (storegen.Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app, ok := m.apps[arg.Name]
	if !ok {
		return storegen.Application{}, fmt.Errorf("app not found: %s", arg.Name)
	}
	delete(m.apps, arg.Name)
	return app, nil
}

func (m *memoryStore) ListExecutorsByApplication(_ context.Context, _ pgtype.UUID) ([]storegen.Executor, error) {
	return nil, nil
}

func (m *memoryStore) ListAPIKeys(_ context.Context, orgID pgtype.UUID) ([]storegen.ApiKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []storegen.ApiKey
	for _, k := range m.keys {
		if k.OrganisationID == orgID && k.RevokedAt.Time.IsZero() {
			res = append(res, k)
		}
	}
	return res, nil
}

func (m *memoryStore) GetAPIKeyByLookup(_ context.Context, lookup string) (storegen.ApiKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range m.keys {
		if k.Lookup == lookup && k.RevokedAt.Time.IsZero() {
			return k, nil
		}
	}
	return storegen.ApiKey{}, fmt.Errorf("key not found: %s", lookup)
}

func (m *memoryStore) CreateAPIKey(_ context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := storegen.ApiKey{
		ID:               m.nextUUID(),
		OrganisationID:   arg.OrganisationID,
		Name:             arg.Name,
		Lookup:           arg.Lookup,
		KeyHash:          arg.KeyHash,
		ApplicationNames: arg.ApplicationNames,
		Permissions:      arg.Permissions,
		CreatedAt:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	m.keys[arg.Lookup] = key
	return key, nil
}

func (m *memoryStore) RevokeAPIKey(_ context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for lookup, k := range m.keys {
		if k.ID == arg.ID && k.OrganisationID == arg.OrganisationID {
			k.RevokedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			m.keys[lookup] = k
			return k, nil
		}
	}
	return storegen.ApiKey{}, fmt.Errorf("key not found")
}

func (m *memoryStore) CreateAlertingRule(_ context.Context, _ storegen.CreateAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{}, nil
}

func (m *memoryStore) GetAlertingRule(_ context.Context, _ storegen.GetAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{}, fmt.Errorf("rule not found")
}

func (m *memoryStore) ListAlertingRulesByApplication(_ context.Context, _ pgtype.UUID) ([]storegen.AlertingRule, error) {
	return nil, nil
}

func (m *memoryStore) DeleteAlertingRule(_ context.Context, _ storegen.DeleteAlertingRuleParams) (int64, error) {
	return 1, nil
}

// Identity methods
func (m *memoryStore) CreateUser(_ context.Context, arg storegen.CreateUserParams) (storegen.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user := storegen.User{
		ID:        m.nextUUID(),
		Subject:   arg.Subject,
		Username:  arg.Username,
		Email:     arg.Email,
		IsAdmin:   arg.IsAdmin,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	m.users[user.Subject] = user
	m.usersByID[user.ID] = user
	return user, nil
}

func (m *memoryStore) UpsertUser(_ context.Context, arg storegen.UpsertUserParams) (storegen.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if user, ok := m.users[arg.Subject]; ok {
		user.Username = arg.Username
		user.Email = arg.Email
		m.users[arg.Subject] = user
		m.usersByID[user.ID] = user
		return user, nil
	}
	user := storegen.User{
		ID:        m.nextUUID(),
		Subject:   arg.Subject,
		Username:  arg.Username,
		Email:     arg.Email,
		IsAdmin:   arg.IsAdmin,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	m.users[user.Subject] = user
	m.usersByID[user.ID] = user
	return user, nil
}

func (m *memoryStore) GetUserBySubject(_ context.Context, subject string) (storegen.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if user, ok := m.users[subject]; ok {
		return user, nil
	}
	return storegen.User{}, fmt.Errorf("user not found: %s", subject)
}

func (m *memoryStore) GetUserByUsername(_ context.Context, username string) (storegen.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return storegen.User{}, fmt.Errorf("user not found: %s", username)
}

func (m *memoryStore) GetUserByID(_ context.Context, id pgtype.UUID) (storegen.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if user, ok := m.usersByID[id]; ok {
		return user, nil
	}
	return storegen.User{}, fmt.Errorf("user not found by ID")
}

func (m *memoryStore) ListMembersByOrganisation(_ context.Context, orgID pgtype.UUID) ([]storegen.ListMembersByOrganisationRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []storegen.ListMembersByOrganisationRow
	for _, member := range m.members {
		if member.OrganisationID == orgID {
			user := m.usersByID[member.UserID]
			res = append(res, storegen.ListMembersByOrganisationRow{
				UserID:    member.UserID,
				Username:  user.Username,
				Email:     user.Email,
				RoleName:  member.RoleName,
				CreatedAt: member.CreatedAt,
			})
		}
	}
	return res, nil
}

func (m *memoryStore) GetMember(_ context.Context, arg storegen.GetMemberParams) (storegen.GetMemberRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, member := range m.members {
		if member.OrganisationID == arg.OrganisationID {
			user := m.usersByID[member.UserID]
			if user.Username == arg.Username {
				return storegen.GetMemberRow{
					UserID:    member.UserID,
					Username:  user.Username,
					Email:     user.Email,
					RoleName:  member.RoleName,
					CreatedAt: member.CreatedAt,
				}, nil
			}
		}
	}
	return storegen.GetMemberRow{}, fmt.Errorf("member not found: %s", arg.Username)
}

func (m *memoryStore) UpsertMemberRole(_ context.Context, arg storegen.UpsertMemberRoleParams) (storegen.OrganisationMember, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s:%s", formatUUID(arg.OrganisationID), formatUUID(arg.UserID))
	if member, ok := m.members[key]; ok {
		member.RoleName = arg.RoleName
		m.members[key] = member
		return member, nil
	}
	member := storegen.OrganisationMember{
		ID:             m.nextUUID(),
		OrganisationID: arg.OrganisationID,
		UserID:         arg.UserID,
		RoleName:       arg.RoleName,
		CreatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	m.members[key] = member
	return member, nil
}

func (m *memoryStore) RemoveMember(_ context.Context, arg storegen.RemoveMemberParams) (storegen.OrganisationMember, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s:%s", formatUUID(arg.OrganisationID), formatUUID(arg.UserID))
	if member, ok := m.members[key]; ok {
		delete(m.members, key)
		return member, nil
	}
	return storegen.OrganisationMember{}, fmt.Errorf("member not found")
}

func (m *memoryStore) GetUserPrimaryOrganisation(_ context.Context, userID pgtype.UUID) (storegen.GetUserPrimaryOrganisationRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, member := range m.members {
		if member.UserID == userID {
			org := m.orgsByID[member.OrganisationID]
			return storegen.GetUserPrimaryOrganisationRow{
				ID:        member.OrganisationID,
				Name:      org.Name,
				CreatedAt: org.CreatedAt,
				RoleName:  member.RoleName,
			}, nil
		}
	}
	return storegen.GetUserPrimaryOrganisationRow{}, fmt.Errorf("primary organisation not found")
}

func (m *memoryStore) ListRoles(_ context.Context, orgID pgtype.UUID) ([]storegen.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []storegen.Role
	for _, r := range m.roles {
		if !r.OrganisationID.Valid || r.OrganisationID == orgID {
			res = append(res, r)
		}
	}
	return res, nil
}

func (m *memoryStore) GetRole(_ context.Context, arg storegen.GetRoleParams) (storegen.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.roles["global:"+arg.Name]; ok {
		return r, nil
	}
	if arg.OrganisationID.Valid {
		if r, ok := m.roles[formatUUID(arg.OrganisationID)+":"+arg.Name]; ok {
			return r, nil
		}
	}
	return storegen.Role{}, fmt.Errorf("role not found: %s", arg.Name)
}

func (m *memoryStore) CreateRole(_ context.Context, arg storegen.CreateRoleParams) (storegen.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := storegen.Role{
		ID:             m.nextUUID(),
		OrganisationID: arg.OrganisationID,
		Name:           arg.Name,
		Permissions:    arg.Permissions,
		CreatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	key := formatUUID(arg.OrganisationID) + ":" + arg.Name
	m.roles[key] = r
	return r, nil
}

func (m *memoryStore) DeleteRole(_ context.Context, arg storegen.DeleteRoleParams) (storegen.Role, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := formatUUID(arg.OrganisationID) + ":" + arg.Name
	if r, ok := m.roles[key]; ok {
		delete(m.roles, key)
		return r, nil
	}
	return storegen.Role{}, fmt.Errorf("custom role not found: %s", arg.Name)
}

func (m *memoryStore) ListDomainClaims(_ context.Context, orgID pgtype.UUID) ([]storegen.DomainClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []storegen.DomainClaim
	for _, dc := range m.domainClaims {
		if dc.OrganisationID == orgID {
			res = append(res, dc)
		}
	}
	return res, nil
}

func (m *memoryStore) GetDomainClaim(_ context.Context, domain string) (storegen.DomainClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dc, ok := m.domainClaims[domain]; ok {
		return dc, nil
	}
	return storegen.DomainClaim{}, fmt.Errorf("domain claim not found: %s", domain)
}

func (m *memoryStore) CreateDomainClaim(_ context.Context, arg storegen.CreateDomainClaimParams) (storegen.DomainClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dc := storegen.DomainClaim{
		ID:             m.nextUUID(),
		OrganisationID: arg.OrganisationID,
		Domain:         arg.Domain,
		CreatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	m.domainClaims[arg.Domain] = dc
	return dc, nil
}

func (m *memoryStore) DeleteDomainClaim(_ context.Context, arg storegen.DeleteDomainClaimParams) (storegen.DomainClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dc, ok := m.domainClaims[arg.Domain]; ok && dc.OrganisationID == arg.OrganisationID {
		delete(m.domainClaims, arg.Domain)
		return dc, nil
	}
	return storegen.DomainClaim{}, fmt.Errorf("domain claim not found: %s", arg.Domain)
}

func (m *memoryStore) CreateAuditLog(_ context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := storegen.AuditLog{
		ID:             m.nextUUID(),
		OrganisationID: arg.OrganisationID,
		UserID:         arg.UserID,
		Username:       arg.Username,
		Action:         arg.Action,
		Details:        arg.Details,
		CreatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	m.auditLogs = append(m.auditLogs, entry)
	return entry, nil
}

func (m *memoryStore) ListAuditLogs(_ context.Context, arg storegen.ListAuditLogsParams) ([]storegen.AuditLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []storegen.AuditLog
	for _, entry := range m.auditLogs {
		if entry.OrganisationID == arg.OrganisationID {
			res = append(res, entry)
		}
	}
	return res, nil
}

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type dummyRouter struct{}

func (d *dummyRouter) Dispatch(_ context.Context, _, _ string, _ protocol.Message) (protocol.Message, error) {
	return nil, nil
}

func setupServer(store *memoryStore, authEnabled bool, validator any) *httptest.Server {
	srv := api.NewServer(&dummyRouter{}, store, slog.Default())
	srv.WithAuth(authEnabled, validator)
	handler := api.NewHandler(store, srv)
	return httptest.NewServer(handler)
}

func TestIdentity_NoAuthModeReturns404ForGatedRoutes(t *testing.T) {
	store := newMemoryStore()
	ts := setupServer(store, false, nil)
	defer ts.Close()

	// Verify that healthz and openapi return 200
	for _, path := range []string{"/healthz", "/openapi.json"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 for %s, got %d", path, resp.StatusCode)
		}
	}

	// 16 OAuth-gated routes that must return 404 in no-auth mode
	gatedCalls := []struct {
		method string
		path   string
		body   string
	}{
		{"GET", "/v2/users/me", ""},
		{"POST", "/v2/users", `{"name":"test"}`},
		{"GET", "/v2/orgs/my-org", ""},
		{"PATCH", "/v2/orgs/my-org", `{"description":"test"}`},
		{"POST", "/v2/orgs/my-org/join", `{"secret":"test"}`},
		{"POST", "/v2/orgs/my-org/secrets", `{"name":"sec","value":"val"}`},
		{"GET", "/v2/orgs/my-org/members", ""},
		{"DELETE", "/v2/orgs/my-org/members/some-user", ""},
		{"PUT", "/v2/orgs/my-org/members/some-user/roles/viewer", ""},
		{"GET", "/v2/orgs/my-org/roles", ""},
		{"POST", "/v2/orgs/my-org/roles", `{"name":"custom","permissions":["application.read"]}`},
		{"DELETE", "/v2/orgs/my-org/roles/custom", ""},
		{"GET", "/v2/orgs/my-org/domain-claims", ""},
		{"POST", "/v2/orgs/my-org/domain-claims", `{"domain":"example.com"}`},
		{"DELETE", "/v2/orgs/my-org/domain-claims/example.com", ""},
		{"GET", "/v2/orgs/my-org/audit-logs", ""},
	}

	for _, tc := range gatedCalls {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body io.Reader
			if tc.body != "" {
				body = bytes.NewBufferString(tc.body)
			}
			req, err := http.NewRequest(tc.method, ts.URL+tc.path, body)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("expected 404 Not Found for %s %s in no-auth mode, got %d", tc.method, tc.path, resp.StatusCode)
			}

			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "application/problem+json") {
				t.Errorf("expected Content-Type application/problem+json, got %s", ct)
			}
		})
	}
}

func TestIdentity_AuthModeValidatesOIDCAndAutoRegisters(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// 1. Without Authorization header -> 401 Unauthorized
	resp, err := http.Get(ts.URL + "/v2/users/me")
	if err != nil {
		t.Fatalf("GET /v2/users/me failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized without token, got %d", resp.StatusCode)
	}

	// 2. Mint valid token
	token := idp.mintToken(t, map[string]any{
		"sub":                "auth0|alice123",
		"email":              "alice@example.com",
		"preferred_username": "alice",
		"iss":                idp.server.URL,
		"aud":                "relay-client",
		"exp":                time.Now().Add(time.Hour).Unix(),
	})

	req, _ := http.NewRequest("GET", ts.URL+"/v2/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v2/users/me with token failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, string(body))
	}

	var userProfile apigen.UserProfile
	if err := json.NewDecoder(resp.Body).Decode(&userProfile); err != nil {
		t.Fatalf("Decode user profile: %v", err)
	}

	if userProfile.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %s", userProfile.Email)
	}
	if userProfile.Name != "alice" {
		t.Errorf("expected name alice, got %s", userProfile.Name)
	}

	// Verify auto-registered in store
	dbUser, err := store.GetUserBySubject(context.Background(), "auth0|alice123")
	if err != nil {
		t.Fatalf("user not found in store: %v", err)
	}
	if dbUser.Email != "alice@example.com" {
		t.Errorf("expected store email alice@example.com, got %s", dbUser.Email)
	}
}

func TestIdentity_FailClosedOnJWKSUnavailable(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// Simulate JWKS outage before validating token
	idp.setFailJWKS(true)

	token := idp.mintToken(t, map[string]any{
		"sub": "some-user",
		"iss": idp.server.URL,
		"aud": "relay-client",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	req, _ := http.NewRequest("GET", ts.URL+"/v2/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized when JWKS fails, got %d", resp.StatusCode)
	}
}

func TestIdentity_TokenFromFakeIdPDeviceFlowAuthenticates(t *testing.T) {
	t.Logf("identity provider: in-test mockIdP stand-in; client: direct HTTP (not dbosctl)")
	idp := newMockIdP(t)
	store := newMemoryStore()

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// 1. Client initiates device code flow
	devCodeResp, err := http.Post(idp.server.URL+"/oauth/device/code", "application/json", nil)
	if err != nil {
		t.Fatalf("Device code request failed: %v", err)
	}
	defer func() { _ = devCodeResp.Body.Close() }()

	var devCodeResult struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	_ = json.NewDecoder(devCodeResp.Body).Decode(&devCodeResult)
	if devCodeResult.DeviceCode == "" {
		t.Fatalf("expected non-empty device code")
	}

	// 2. Client polls token endpoint
	tokenReqBody, _ := json.Marshal(map[string]string{
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		"device_code": devCodeResult.DeviceCode,
	})
	tokenResp, err := http.Post(idp.server.URL+"/oauth/token", "application/json", bytes.NewReader(tokenReqBody))
	if err != nil {
		t.Fatalf("Token poll request failed: %v", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()

	var tokenResult struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	_ = json.NewDecoder(tokenResp.Body).Decode(&tokenResult)
	if tokenResult.AccessToken == "" {
		t.Fatalf("expected non-empty access_token")
	}

	// 3. Client uses token to authenticate against Relay
	req, _ := http.NewRequest("GET", ts.URL+"/v2/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+tokenResult.AccessToken)

	relayResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Relay request failed: %v", err)
	}
	defer func() { _ = relayResp.Body.Close() }()

	if relayResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK from Relay, got %d", relayResp.StatusCode)
	}
}

func TestIdentity_DomainClaimAutoEnrollment(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()

	// Pre-create organisation and domain claim for acme.corp
	org, err := store.UpsertOrganisation(context.Background(), "acme")
	if err != nil {
		t.Fatalf("UpsertOrganisation: %v", err)
	}

	_, err = store.CreateDomainClaim(context.Background(), storegen.CreateDomainClaimParams{
		OrganisationID: org.ID,
		Domain:         "acme.corp",
	})
	if err != nil {
		t.Fatalf("CreateDomainClaim: %v", err)
	}

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// User logs in with email at claimed domain
	token := idp.mintToken(t, map[string]any{
		"sub":                "sub-bob-acme",
		"email":              "bob@acme.corp",
		"preferred_username": "bob",
		"iss":                idp.server.URL,
		"aud":                "relay-client",
		"exp":                time.Now().Add(time.Hour).Unix(),
	})

	req, _ := http.NewRequest("GET", ts.URL+"/v2/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /v2/users/me failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	// Verify user is enrolled as member of acme org with role 'viewer'
	user, err := store.GetUserBySubject(context.Background(), "sub-bob-acme")
	if err != nil {
		t.Fatalf("GetUserBySubject: %v", err)
	}

	member, err := store.GetMember(context.Background(), storegen.GetMemberParams{
		OrganisationID: org.ID,
		Username:       user.Username,
	})
	if err != nil {
		t.Fatalf("User was not auto-enrolled in domain-claimed organisation: %v", err)
	}
	if member.RoleName != auth.RoleViewer {
		t.Errorf("expected role %s, got %s", auth.RoleViewer, member.RoleName)
	}
}

func TestIdentity_RolesLifecycle(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()

	org, _ := store.UpsertOrganisation(context.Background(), "acme")
	user, _ := store.UpsertUser(context.Background(), storegen.UpsertUserParams{
		Subject:  "sub-admin",
		Username: "admin",
		Email:    "admin@acme.corp",
	})
	_, err := store.UpsertMemberRole(context.Background(), storegen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		RoleName:       auth.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("failed to insert role: %v", err)
	}

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	token := idp.mintToken(t, map[string]any{
		"sub":                "sub-admin",
		"email":              "admin@acme.corp",
		"preferred_username": "admin",
		"iss":                idp.server.URL,
		"aud":                "relay-client",
		"exp":                time.Now().Add(time.Hour).Unix(),
	})

	// 1. List roles: initially has global roles (admin, operator, viewer)
	req, _ := http.NewRequest("GET", ts.URL+"/v2/orgs/acme/roles", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var roles []apigen.RoleOutput
	_ = json.NewDecoder(resp.Body).Decode(&roles)
	if len(roles) < 3 {
		t.Errorf("expected at least 3 default roles, got %d", len(roles))
	}

	// 2. Create custom role
	perms := []string{auth.PermApplicationRead}
	createRoleBody, _ := json.Marshal(apigen.CreateRoleJSONRequestBody{
		Name:        "auditor",
		Permissions: &perms,
	})
	req, _ = http.NewRequest("POST", ts.URL+"/v2/orgs/acme/roles", bytes.NewReader(createRoleBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 201 Created or 200 OK on CreateRole, got %d", resp.StatusCode)
	}

	// 3. Delete custom role
	req, _ = http.NewRequest("DELETE", ts.URL+"/v2/orgs/acme/roles/auditor", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 No Content on DeleteRole, got %d", resp.StatusCode)
	}
	_ = org
}

func TestIdentity_DomainClaimsLifecycle(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()

	org, _ := store.UpsertOrganisation(context.Background(), "acme")
	user, _ := store.UpsertUser(context.Background(), storegen.UpsertUserParams{
		Subject:  "admin-sub",
		Username: "admin-sub",
		Email:    "admin@acme.corp",
	})
	_, err := store.UpsertMemberRole(context.Background(), storegen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		RoleName:       auth.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("failed to insert role: %v", err)
	}

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	token := idp.mintToken(t, map[string]any{
		"sub": "admin-sub",
		"iss": idp.server.URL,
		"aud": "relay-client",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	// 1. Request domain claim
	body, _ := json.Marshal(apigen.RequestDomainClaimJSONRequestBody{
		Domain: "dev.acme.corp",
	})
	req, _ := http.NewRequest("POST", ts.URL+"/v2/orgs/acme/domain-claims", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("RequestDomainClaim: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 201 Created or 200 OK on RequestDomainClaim, got %d", resp.StatusCode)
	}

	// 2. List domain claims
	req, _ = http.NewRequest("GET", ts.URL+"/v2/orgs/acme/domain-claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ListDomainClaims: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var res struct {
		Claims []apigen.DomainClaim `json:"claims"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	claims := res.Claims
	if len(claims) != 1 || claims[0].Domain != "dev.acme.corp" {
		t.Errorf("expected 1 claim for dev.acme.corp, got %+v", claims)
	}

	// 3. Release domain claim
	req, _ = http.NewRequest("DELETE", ts.URL+"/v2/orgs/acme/domain-claims/dev.acme.corp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ReleaseDomainClaim: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 No Content on ReleaseDomainClaim, got %d", resp.StatusCode)
	}
}
func TestIdentity_WriteHandlers_EnforceAdmin(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()

	org, _ := store.UpsertOrganisation(context.Background(), "acme")
	user, _ := store.UpsertUser(context.Background(), storegen.UpsertUserParams{
		Subject:  "viewer-sub",
		Username: "viewer-sub",
		Email:    "viewer@acme.corp",
	})
	// Grant them Viewer role
	_, err := store.UpsertMemberRole(context.Background(), storegen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		RoleName:       auth.RoleViewer,
	})
	if err != nil {
		t.Fatalf("failed to insert role: %v", err)
	}

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	token := idp.mintToken(t, map[string]any{
		"sub": "viewer-sub",
		"iss": idp.server.URL,
		"aud": "relay-client",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{"POST", "/v2/orgs/acme/roles", `{"name": "test-role"}`},
		{"DELETE", "/v2/orgs/acme/roles/test-role", ""},
		{"PUT", "/v2/orgs/acme/members/someuser/roles/operator", ""},
		{"DELETE", "/v2/orgs/acme/members/someuser", ""},
		{"PATCH", "/v2/orgs/acme", "{}"},
		{"POST", "/v2/orgs/acme/secrets", "{}"},
		{"POST", "/v2/orgs/acme/domain-claims", `{"domain": "foo.com"}`},
		{"DELETE", "/v2/orgs/acme/domain-claims/foo.com", ""},
	}

	for _, ep := range endpoints {
		var reqBody io.Reader
		if ep.body != "" {
			reqBody = bytes.NewReader([]byte(ep.body))
		}
		req, _ := http.NewRequest(ep.method, ts.URL+ep.path, reqBody)
		req.Header.Set("Authorization", "Bearer "+token)
		if ep.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed for %s %s: %v", ep.method, ep.path, err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for %s %s, got %d", ep.method, ep.path, resp.StatusCode)
		}
	}
}

func (m *memoryStore) TouchAPIKeyLastUsed(ctx context.Context, id pgtype.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.touchedKeys == nil {
		m.touchedKeys = make(map[pgtype.UUID]time.Time)
	}
	m.touchedKeys[id] = time.Now()
	return nil
}

func TestIdentity_APIKey_TouchLastUsed(t *testing.T) {
	store := newMemoryStore()
	org, _ := store.UpsertOrganisation(context.Background(), "acme")

	plain, rec, err := auth.Mint()
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	apiKey, err := store.CreateAPIKey(context.Background(), storegen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             "test-key",
		Lookup:           rec.Lookup,
		KeyHash:          rec.Hash,
		ApplicationNames: []string{},
		Permissions:      []string{auth.PermApplicationRead},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	ts := setupServer(store, true, nil)
	defer ts.Close()

	// 1. Successful authentication touches last_used_at
	req, _ := http.NewRequest("GET", ts.URL+"/v2/orgs/acme/apps", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	store.mu.Lock()
	_, touched := store.touchedKeys[apiKey.ID]
	store.mu.Unlock()
	if !touched {
		t.Error("expected TouchAPIKeyLastUsed to be called on successful authentication")
	}

	// 2. Unauthenticated / invalid key does NOT touch last_used_at
	store.mu.Lock()
	store.touchedKeys = make(map[pgtype.UUID]time.Time)
	store.mu.Unlock()

	reqBad, _ := http.NewRequest("GET", ts.URL+"/v2/orgs/acme/apps", nil)
	reqBad.Header.Set("Authorization", "Bearer "+auth.KeyPrefix+"badkeymaterial12345678901234567890")
	respBad, err := http.DefaultClient.Do(reqBad)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = respBad.Body.Close()
	if respBad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", respBad.StatusCode)
	}

	store.mu.Lock()
	touchCount := len(store.touchedKeys)
	store.mu.Unlock()
	if touchCount != 0 {
		t.Error("expected TouchAPIKeyLastUsed NOT to be called on failed authentication")
	}
}

func TestIdentity_DomainClaims_CrossOrgRejected(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()

	orgA, _ := store.UpsertOrganisation(context.Background(), "org-a")
	if _, err := store.UpsertOrganisation(context.Background(), "org-b"); err != nil {
		t.Fatalf("failed to upsert org-b: %v", err)
	}

	userA, _ := store.UpsertUser(context.Background(), storegen.UpsertUserParams{
		Subject:  "user-a-sub",
		Username: "user-a",
		Email:    "user-a@org-a.corp",
	})
	// user-a is admin of org-a only
	_, _ = store.UpsertMemberRole(context.Background(), storegen.UpsertMemberRoleParams{
		OrganisationID: orgA.ID,
		UserID:         userA.ID,
		RoleName:       auth.RoleAdmin,
	})

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	tokenA := idp.mintToken(t, map[string]any{
		"sub": "user-a-sub",
		"iss": idp.server.URL,
		"aud": "relay-client",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	// User in org A requesting domain claim on org B must be rejected with 403 Forbidden
	reqBody := strings.NewReader(`{"domain": "org-b.com"}`)
	req, _ := http.NewRequest("POST", ts.URL+"/v2/orgs/org-b/domain-claims", reqBody)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for cross-org domain claim, got %d", resp.StatusCode)
	}

	// User in org A requesting domain claim on org A succeeds with 201 Created
	reqBodyOk := strings.NewReader(`{"domain": "org-a.com"}`)
	reqOk, _ := http.NewRequest("POST", ts.URL+"/v2/orgs/org-a/domain-claims", reqBodyOk)
	reqOk.Header.Set("Authorization", "Bearer "+tokenA)
	reqOk.Header.Set("Content-Type", "application/json")
	respOk, err := http.DefaultClient.Do(reqOk)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = respOk.Body.Close()
	if respOk.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for in-org domain claim, got %d", respOk.StatusCode)
	}
}

func TestIdentity_WriteHandlers_Execution(t *testing.T) {
	idp := newMockIdP(t)
	store := newMemoryStore()

	org, _ := store.UpsertOrganisation(context.Background(), "acme")

	adminUser, _ := store.UpsertUser(context.Background(), storegen.UpsertUserParams{
		Subject:  "admin-sub",
		Username: "admin-user",
		Email:    "admin@acme.corp",
	})
	_, _ = store.UpsertMemberRole(context.Background(), storegen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         adminUser.ID,
		RoleName:       auth.RoleAdmin,
	})

	memberUser, _ := store.UpsertUser(context.Background(), storegen.UpsertUserParams{
		Subject:  "member-sub",
		Username: "member-user",
		Email:    "member@acme.corp",
	})
	_, _ = store.UpsertMemberRole(context.Background(), storegen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         memberUser.ID,
		RoleName:       auth.RoleViewer,
	})

	joinerUser, _ := store.UpsertUser(context.Background(), storegen.UpsertUserParams{
		Subject:  "joiner-sub",
		Username: "joiner-user",
		Email:    "joiner@external.com",
	})

	validator := auth.NewOIDCValidator(idp.server.URL, "relay-client", idp.server.Client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	adminToken := idp.mintToken(t, map[string]any{
		"sub": "admin-sub",
		"iss": idp.server.URL,
		"aud": "relay-client",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	joinerToken := idp.mintToken(t, map[string]any{
		"sub": "joiner-sub",
		"iss": idp.server.URL,
		"aud": "relay-client",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	// 1. Generate org secret (handleGenerateSecret)
	secReq, _ := http.NewRequest("POST", ts.URL+"/v2/orgs/acme/secrets", strings.NewReader("{}"))
	secReq.Header.Set("Authorization", "Bearer "+adminToken)
	secReq.Header.Set("Content-Type", "application/json")
	secResp, err := http.DefaultClient.Do(secReq)
	if err != nil {
		t.Fatalf("generate secret failed: %v", err)
	}
	defer func() { _ = secResp.Body.Close() }()
	if secResp.StatusCode != http.StatusCreated && secResp.StatusCode != http.StatusOK {
		t.Fatalf("generate secret: expected 201 or 200, got %d", secResp.StatusCode)
	}
	var secBody struct {
		Secret string `json:"secret"`
	}
	_ = json.NewDecoder(secResp.Body).Decode(&secBody)
	if secBody.Secret == "" {
		t.Fatal("expected non-empty secret in response")
	}

	// 2. Join org with invalid secret -> 403 (handleJoinOrg)
	badJoinReq, _ := http.NewRequest("POST", ts.URL+"/v2/orgs/acme/join", strings.NewReader(`{"secret":"wrong-secret"}`))
	badJoinReq.Header.Set("Authorization", "Bearer "+joinerToken)
	badJoinReq.Header.Set("Content-Type", "application/json")
	badJoinResp, err := http.DefaultClient.Do(badJoinReq)
	if err != nil {
		t.Fatalf("join request failed: %v", err)
	}
	_ = badJoinResp.Body.Close()
	if badJoinResp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad join secret: expected 403, got %d", badJoinResp.StatusCode)
	}

	// 3. Join org with valid secret -> 200 OK (handleJoinOrg)
	joinReq, _ := http.NewRequest("POST", ts.URL+"/v2/orgs/acme/join", strings.NewReader(fmt.Sprintf(`{"secret":%q}`, secBody.Secret)))
	joinReq.Header.Set("Authorization", "Bearer "+joinerToken)
	joinReq.Header.Set("Content-Type", "application/json")
	joinResp, err := http.DefaultClient.Do(joinReq)
	if err != nil {
		t.Fatalf("join request failed: %v", err)
	}
	_ = joinResp.Body.Close()
	if joinResp.StatusCode != http.StatusNoContent && joinResp.StatusCode != http.StatusOK {
		t.Fatalf("valid join secret: expected 204 or 200, got %d", joinResp.StatusCode)
	}

	// 4. Grant role -> 204 No Content (handleGrantRole)
	grantReq, _ := http.NewRequest("PUT", ts.URL+"/v2/orgs/acme/members/member-user/roles/operator", nil)
	grantReq.Header.Set("Authorization", "Bearer "+adminToken)
	grantResp, err := http.DefaultClient.Do(grantReq)
	if err != nil {
		t.Fatalf("grant role failed: %v", err)
	}
	_ = grantResp.Body.Close()
	if grantResp.StatusCode != http.StatusNoContent {
		t.Fatalf("grant role: expected 204, got %d", grantResp.StatusCode)
	}

	// 5. Remove member -> 204 No Content (handleRemoveMember)
	delReq, _ := http.NewRequest("DELETE", ts.URL+"/v2/orgs/acme/members/member-user", nil)
	delReq.Header.Set("Authorization", "Bearer "+adminToken)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("remove member failed: %v", err)
	}
	_ = delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent && delResp.StatusCode != http.StatusOK {
		t.Fatalf("remove member: expected 204/200, got %d", delResp.StatusCode)
	}

	// 6. List audit logs (handleListAuditLogs)
	_, _ = store.CreateAuditLog(context.Background(), storegen.CreateAuditLogParams{
		OrganisationID: org.ID,
		UserID:         adminUser.ID,
		Action:         "generateSecret",
		Details:        []byte(`{"status":"success","sourceIp":"10.0.0.1"}`),
	})
	auditReq, _ := http.NewRequest("GET", ts.URL+"/v2/orgs/acme/audit-logs", nil)
	auditReq.Header.Set("Authorization", "Bearer "+adminToken)
	auditResp, err := http.DefaultClient.Do(auditReq)
	if err != nil {
		t.Fatalf("list audit logs failed: %v", err)
	}
	_ = auditResp.Body.Close()
	if auditResp.StatusCode != http.StatusOK {
		t.Fatalf("list audit logs: expected 200, got %d", auditResp.StatusCode)
	}

	// 7. Unknown org audit logs -> 404 Not Found
	badAuditReq, _ := http.NewRequest("GET", ts.URL+"/v2/orgs/nonexistent-org/audit-logs", nil)
	badAuditReq.Header.Set("Authorization", "Bearer "+adminToken)
	badAuditResp, err := http.DefaultClient.Do(badAuditReq)
	if err != nil {
		t.Fatalf("unknown org audit logs failed: %v", err)
	}
	_ = badAuditResp.Body.Close()
	if badAuditResp.StatusCode != http.StatusNotFound && badAuditResp.StatusCode != http.StatusForbidden {
		t.Fatalf("unknown org audit logs: expected 404 or 403, got %d", badAuditResp.StatusCode)
	}
	_ = joinerUser
}
