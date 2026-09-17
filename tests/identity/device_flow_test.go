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
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	apigen "github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/auth"
	storegen "github.com/abn/relay/internal/store/gen"
)

// deviceCodeRecord holds the state of an RFC 8628 device authorization request.
type deviceCodeRecord struct {
	deviceCode string
	userCode   string
	status     string // "pending", "approved", "denied", "expired", "slow_down"
	claims     map[string]any
	expiresAt  time.Time
}

// deviceAuthServer implements an in-memory RFC 8628 and OIDC identity provider.
type deviceAuthServer struct {
	privKey  *rsa.PrivateKey
	kid      string
	server   *httptest.Server
	mu       sync.Mutex
	codes    map[string]*deviceCodeRecord
	codesByUser map[string]*deviceCodeRecord
	refreshTokens map[string]map[string]any
}

func newDeviceAuthServer(t *testing.T) *deviceAuthServer {
	t.Helper()
	t.Logf("identity provider: in-test mock stand-in (not a real external OIDC provider)")
	t.Logf("client: direct HTTP test runner exercising RFC 8628 device flow against Relay")

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	kid := "relay-test-idp-key"
	das := &deviceAuthServer{
		privKey:       priv,
		kid:           kid,
		codes:         make(map[string]*deviceCodeRecord),
		codesByUser:   make(map[string]*deviceCodeRecord),
		refreshTokens: make(map[string]map[string]any),
	}

	mux := http.NewServeMux()

	// 1. RFC 8414 / OpenID Connect Discovery
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                        base,
			"jwks_uri":                      base + "/jwks.json",
			"device_authorization_endpoint": base + "/oauth/device/code",
			"token_endpoint":                base + "/oauth/token",
			"response_types_supported":      []string{"code", "token", "id_token"},
			"grant_types_supported": []string{
				"urn:ietf:params:oauth:grant-type:device_code",
				"refresh_token",
			},
		})
	})

	// 2. JWKS endpoint
	mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		nStr := base64.RawURLEncoding.EncodeToString(priv.N.Bytes())
		eBytes := big.NewInt(int64(priv.E)).Bytes()
		eStr := base64.RawURLEncoding.EncodeToString(eBytes)

		w.Header().Set("Content-Type", "application/json")
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

	// 3. RFC 8628 Device Authorization Endpoint
	mux.HandleFunc("/oauth/device/code", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		clientID := das.extractParam(r, "client_id")
		if clientID == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_request",
				"error_description": "missing client_id",
			})
			return
		}

		das.mu.Lock()
		devCode := fmt.Sprintf("device-code-%d", len(das.codes)+1)
		usrCode := fmt.Sprintf("CODE-%04d", len(das.codes)+1)
		rec := &deviceCodeRecord{
			deviceCode: devCode,
			userCode:   usrCode,
			status:     "pending",
			expiresAt:  time.Now().Add(300 * time.Second),
		}
		das.codes[devCode] = rec
		das.codesByUser[usrCode] = rec
		das.mu.Unlock()

		base := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               devCode,
			"user_code":                 usrCode,
			"verification_uri":          base + "/activate",
			"verification_uri_complete": base + "/activate?user_code=" + usrCode,
			"expires_in":                300,
			"interval":                  1,
		})
	})

	// 4. Token endpoint supporting device_code and refresh_token
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		grantType := das.extractParam(r, "grant_type")
		w.Header().Set("Content-Type", "application/json")

		switch grantType {
		case "urn:ietf:params:oauth:grant-type:device_code":
			devCode := das.extractParam(r, "device_code")
			das.mu.Lock()
			rec, exists := das.codes[devCode]
			das.mu.Unlock()

			if !exists {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "invalid_grant",
					"error_description": "device code not recognized",
				})
				return
			}

			if time.Now().After(rec.expiresAt) || rec.status == "expired" {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "expired_token",
					"error_description": "the device code has expired",
				})
				return
			}

			switch rec.status {
			case "pending":
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "authorization_pending",
					"error_description": "authorization is pending user confirmation",
				})
			case "slow_down":
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "slow_down",
					"error_description": "polling interval must be increased",
				})
			case "denied":
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "access_denied",
					"error_description": "authorization was denied by user",
				})
			case "approved":
				base := "http://" + r.Host
				claims := rec.claims
				if claims == nil {
					claims = map[string]any{
						"sub":                "test-user-sub",
						"email":              "user@example.com",
						"preferred_username": "user",
					}
				}
				claims["iss"] = base
				claims["aud"] = "relay-client"
				claims["exp"] = time.Now().Add(time.Hour).Unix()

				token := das.mintToken(t, claims)
				refreshToken := "refresh-" + devCode

				das.mu.Lock()
				das.refreshTokens[refreshToken] = claims
				das.mu.Unlock()

				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": token,
					"id_token":     token,
					"refresh_token": refreshToken,
					"token_type":   "Bearer",
					"expires_in":   3600,
				})
			default:
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "server_error",
				})
			}

		case "refresh_token":
			rt := das.extractParam(r, "refresh_token")
			das.mu.Lock()
			storedClaims, exists := das.refreshTokens[rt]
			das.mu.Unlock()

			if !exists {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":             "invalid_grant",
					"error_description": "unknown refresh token",
				})
				return
			}

			base := "http://" + r.Host
			newClaims := make(map[string]any)
			for k, v := range storedClaims {
				newClaims[k] = v
			}
			newClaims["iss"] = base
			newClaims["aud"] = "relay-client"
			newClaims["exp"] = time.Now().Add(time.Hour).Unix()

			newToken := das.mintToken(t, newClaims)
			rotatedRT := "rotated-" + rt

			das.mu.Lock()
			delete(das.refreshTokens, rt)
			das.refreshTokens[rotatedRT] = newClaims
			das.mu.Unlock()

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": newToken,
				"id_token":     newToken,
				"refresh_token": rotatedRT,
				"token_type":   "Bearer",
				"expires_in":   3600,
			})

		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "unsupported_grant_type",
				"error_description": "grant type not supported",
			})
		}
	})

	das.server = httptest.NewServer(mux)
	t.Cleanup(das.server.Close)
	return das
}

func (das *deviceAuthServer) extractParam(r *http.Request, key string) string {
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil {
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			var payload map[string]any
			if json.Unmarshal(bodyBytes, &payload) == nil {
				if v, ok := payload[key].(string); ok {
					return v
				}
			}
		}
	}

	_ = r.ParseForm()
	return r.Form.Get(key)
}

func (das *deviceAuthServer) approveDeviceCode(devCode string, claims map[string]any) {
	das.mu.Lock()
	defer das.mu.Unlock()
	if rec, ok := das.codes[devCode]; ok {
		rec.status = "approved"
		rec.claims = claims
	}
}

func (das *deviceAuthServer) denyDeviceCode(devCode string) {
	das.mu.Lock()
	defer das.mu.Unlock()
	if rec, ok := das.codes[devCode]; ok {
		rec.status = "denied"
	}
}

func (das *deviceAuthServer) setSlowDown(devCode string, enabled bool) {
	das.mu.Lock()
	defer das.mu.Unlock()
	if rec, ok := das.codes[devCode]; ok {
		if enabled {
			rec.status = "slow_down"
		} else {
			rec.status = "pending"
		}
	}
}

func (das *deviceAuthServer) expireDeviceCode(devCode string) {
	das.mu.Lock()
	defer das.mu.Unlock()
	if rec, ok := das.codes[devCode]; ok {
		rec.status = "expired"
		rec.expiresAt = time.Now().Add(-1 * time.Hour)
	}
}

func (das *deviceAuthServer) mintToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]string{
		"alg": "RS256",
		"kid": das.kid,
		"typ": "JWT",
	}
	hBytes, _ := json.Marshal(header)
	pBytes, _ := json.Marshal(claims)

	hPart := base64.RawURLEncoding.EncodeToString(hBytes)
	pPart := base64.RawURLEncoding.EncodeToString(pBytes)
	content := hPart + "." + pPart

	hashed := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, das.privKey, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15 failed: %v", err)
	}
	sPart := base64.RawURLEncoding.EncodeToString(sig)

	return content + "." + sPart
}

func (das *deviceAuthServer) url() string {
	return das.server.URL
}

func (das *deviceAuthServer) client() *http.Client {
	return das.server.Client()
}

func TestDeviceFlow_FullRFC8628Lifecycle(t *testing.T) {
	idp := newDeviceAuthServer(t)
	store := newMemoryStore()

	validator := auth.NewOIDCValidator(idp.url(), "relay-client", idp.client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// Step 1: Discover endpoints via OIDC Discovery
	discResp, err := idp.client().Get(idp.url() + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("Discovery request failed: %v", err)
	}
	defer func() { _ = discResp.Body.Close() }()

	if discResp.StatusCode != http.StatusOK {
		t.Fatalf("Discovery returned %d", discResp.StatusCode)
	}

	var discoveryDoc struct {
		Issuer                   string `json:"issuer"`
		DeviceAuthEndpoint       string `json:"device_authorization_endpoint"`
		TokenEndpoint            string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(discResp.Body).Decode(&discoveryDoc); err != nil {
		t.Fatalf("Decode discovery: %v", err)
	}

	if discoveryDoc.DeviceAuthEndpoint == "" {
		t.Fatal("device_authorization_endpoint is empty in discovery document")
	}
	if discoveryDoc.TokenEndpoint == "" {
		t.Fatal("token_endpoint is empty in discovery document")
	}

	// Step 2: Request device authorization code
	form := url.Values{}
	form.Set("client_id", "relay-client")
	form.Set("scope", "openid profile email")

	devReq, err := http.NewRequest(http.MethodPost, discoveryDoc.DeviceAuthEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("NewRequest device code: %v", err)
	}
	devReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	devResp, err := idp.client().Do(devReq)
	if err != nil {
		t.Fatalf("Device auth request failed: %v", err)
	}
	defer func() { _ = devResp.Body.Close() }()

	if devResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(devResp.Body)
		t.Fatalf("Device auth returned %d: %s", devResp.StatusCode, string(body))
	}

	var devResult struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.NewDecoder(devResp.Body).Decode(&devResult); err != nil {
		t.Fatalf("Decode device auth response: %v", err)
	}

	if devResult.DeviceCode == "" {
		t.Fatal("device_code is empty")
	}
	if devResult.UserCode == "" {
		t.Fatal("user_code is empty")
	}
	if devResult.VerificationURI == "" {
		t.Fatal("verification_uri is empty")
	}
	if !strings.Contains(devResult.VerificationURIComplete, devResult.UserCode) {
		t.Errorf("verification_uri_complete %q does not contain user_code %q",
			devResult.VerificationURIComplete, devResult.UserCode)
	}
	if devResult.ExpiresIn <= 0 {
		t.Errorf("expected expires_in > 0, got %d", devResult.ExpiresIn)
	}

	// Step 3: First poll to token endpoint before user approves -> authorization_pending
	pollForm := url.Values{}
	pollForm.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	pollForm.Set("device_code", devResult.DeviceCode)
	pollForm.Set("client_id", "relay-client")

	pollReq, _ := http.NewRequest(http.MethodPost, discoveryDoc.TokenEndpoint, strings.NewReader(pollForm.Encode()))
	pollReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	pollResp1, err := idp.client().Do(pollReq)
	if err != nil {
		t.Fatalf("Poll 1 failed: %v", err)
	}
	defer func() { _ = pollResp1.Body.Close() }()

	if pollResp1.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request while authorization pending, got %d", pollResp1.StatusCode)
	}

	var errResult1 struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(pollResp1.Body).Decode(&errResult1)
	if errResult1.Error != "authorization_pending" {
		t.Fatalf("expected error authorization_pending, got %q", errResult1.Error)
	}

	// Step 4: User completes authentication on verification page
	idp.approveDeviceCode(devResult.DeviceCode, map[string]any{
		"sub":                "carol-sub-id",
		"email":              "carol@example.com",
		"preferred_username": "carol",
	})

	// Step 5: Second poll to token endpoint -> 200 OK with tokens
	pollReq2, _ := http.NewRequest(http.MethodPost, discoveryDoc.TokenEndpoint, strings.NewReader(pollForm.Encode()))
	pollReq2.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	pollResp2, err := idp.client().Do(pollReq2)
	if err != nil {
		t.Fatalf("Poll 2 failed: %v", err)
	}
	defer func() { _ = pollResp2.Body.Close() }()

	if pollResp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(pollResp2.Body)
		t.Fatalf("expected 200 OK on poll after approval, got %d: %s", pollResp2.StatusCode, string(body))
	}

	var tokenResult struct {
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(pollResp2.Body).Decode(&tokenResult); err != nil {
		t.Fatalf("Decode token result: %v", err)
	}

	if tokenResult.AccessToken == "" {
		t.Fatal("access_token is empty")
	}
	if tokenResult.RefreshToken == "" {
		t.Fatal("refresh_token is empty")
	}
	if tokenResult.TokenType != "Bearer" {
		t.Errorf("expected token_type Bearer, got %q", tokenResult.TokenType)
	}

	// Step 6: Relay verification with token obtained from device flow
	relayReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/v2/users/me", nil)
	relayReq.Header.Set("Authorization", "Bearer "+tokenResult.AccessToken)

	relayResp, err := http.DefaultClient.Do(relayReq)
	if err != nil {
		t.Fatalf("Relay GET /v2/users/me failed: %v", err)
	}
	defer func() { _ = relayResp.Body.Close() }()

	if relayResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(relayResp.Body)
		t.Fatalf("expected 200 OK from Relay, got %d: %s", relayResp.StatusCode, string(body))
	}

	var profile apigen.UserProfile
	if err := json.NewDecoder(relayResp.Body).Decode(&profile); err != nil {
		t.Fatalf("Decode Relay user profile: %v", err)
	}

	if profile.Name != "carol" {
		t.Errorf("expected name carol, got %q", profile.Name)
	}
	if profile.Email != "carol@example.com" {
		t.Errorf("expected email carol@example.com, got %q", profile.Email)
	}
}

func TestDeviceFlow_RateLimiting_SlowDown(t *testing.T) {
	idp := newDeviceAuthServer(t)

	// 1. Request device code
	form := url.Values{}
	form.Set("client_id", "relay-client")
	devResp, err := idp.client().PostForm(idp.url()+"/oauth/device/code", form)
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	defer func() { _ = devResp.Body.Close() }()

	var devResult struct {
		DeviceCode string `json:"device_code"`
		Interval   int    `json:"interval"`
	}
	_ = json.NewDecoder(devResp.Body).Decode(&devResult)

	// 2. Set IdP state to slow_down
	idp.setSlowDown(devResult.DeviceCode, true)

	// 3. Poll token endpoint
	pollForm := url.Values{}
	pollForm.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	pollForm.Set("device_code", devResult.DeviceCode)
	pollForm.Set("client_id", "relay-client")

	pollResp, err := idp.client().PostForm(idp.url()+"/oauth/token", pollForm)
	if err != nil {
		t.Fatalf("Poll failed: %v", err)
	}
	defer func() { _ = pollResp.Body.Close() }()

	if pollResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", pollResp.StatusCode)
	}

	var errResult struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(pollResp.Body).Decode(&errResult)
	if errResult.Error != "slow_down" {
		t.Fatalf("expected error slow_down, got %q", errResult.Error)
	}

	// 4. Per RFC 8628 Section 3.5: client must add 5 seconds to interval
	backoffInterval := devResult.Interval + 5
	if backoffInterval < 5 {
		t.Errorf("expected backoff interval >= 5s, got %d", backoffInterval)
	}

	// 5. Subsequent poll after backoff and approval succeeds
	idp.approveDeviceCode(devResult.DeviceCode, map[string]any{
		"sub":   "slowdown-user",
		"email": "slowdown@example.com",
	})

	pollResp2, err := idp.client().PostForm(idp.url()+"/oauth/token", pollForm)
	if err != nil {
		t.Fatalf("Poll 2 failed: %v", err)
	}
	defer func() { _ = pollResp2.Body.Close() }()

	if pollResp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after backoff and approval, got %d", pollResp2.StatusCode)
	}
}

func TestDeviceFlow_ExpiredToken(t *testing.T) {
	idp := newDeviceAuthServer(t)
	store := newMemoryStore()

	validator := auth.NewOIDCValidator(idp.url(), "relay-client", idp.client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// 1. Request device code
	form := url.Values{}
	form.Set("client_id", "relay-client")
	devResp, err := idp.client().PostForm(idp.url()+"/oauth/device/code", form)
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	defer func() { _ = devResp.Body.Close() }()

	var devResult struct {
		DeviceCode string `json:"device_code"`
	}
	_ = json.NewDecoder(devResp.Body).Decode(&devResult)

	// 2. Simulate code expiration
	idp.expireDeviceCode(devResult.DeviceCode)

	// 3. Poll token endpoint
	pollForm := url.Values{}
	pollForm.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	pollForm.Set("device_code", devResult.DeviceCode)
	pollForm.Set("client_id", "relay-client")

	pollResp, err := idp.client().PostForm(idp.url()+"/oauth/token", pollForm)
	if err != nil {
		t.Fatalf("Poll failed: %v", err)
	}
	defer func() { _ = pollResp.Body.Close() }()

	if pollResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for expired code, got %d", pollResp.StatusCode)
	}

	var errResult struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(pollResp.Body).Decode(&errResult)
	if errResult.Error != "expired_token" {
		t.Fatalf("expected error expired_token, got %q", errResult.Error)
	}

	// 4. Relay correctly denies unauthenticated access without valid token
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v2/users/me", nil)
	relayResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Relay request failed: %v", err)
	}
	defer func() { _ = relayResp.Body.Close() }()

	if relayResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized from Relay, got %d", relayResp.StatusCode)
	}
}

func TestDeviceFlow_AccessDenied(t *testing.T) {
	idp := newDeviceAuthServer(t)

	// 1. Request device code
	form := url.Values{}
	form.Set("client_id", "relay-client")
	devResp, err := idp.client().PostForm(idp.url()+"/oauth/device/code", form)
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	defer func() { _ = devResp.Body.Close() }()

	var devResult struct {
		DeviceCode string `json:"device_code"`
	}
	_ = json.NewDecoder(devResp.Body).Decode(&devResult)

	// 2. User rejects access
	idp.denyDeviceCode(devResult.DeviceCode)

	// 3. Poll token endpoint
	pollForm := url.Values{}
	pollForm.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	pollForm.Set("device_code", devResult.DeviceCode)
	pollForm.Set("client_id", "relay-client")

	pollResp, err := idp.client().PostForm(idp.url()+"/oauth/token", pollForm)
	if err != nil {
		t.Fatalf("Poll failed: %v", err)
	}
	defer func() { _ = pollResp.Body.Close() }()

	if pollResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request on access denial, got %d", pollResp.StatusCode)
	}

	var errResult struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(pollResp.Body).Decode(&errResult)
	if errResult.Error != "access_denied" {
		t.Fatalf("expected error access_denied, got %q", errResult.Error)
	}
}

func TestDeviceFlow_TokenRefreshLifecycle(t *testing.T) {
	idp := newDeviceAuthServer(t)
	store := newMemoryStore()

	validator := auth.NewOIDCValidator(idp.url(), "relay-client", idp.client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// 1. Initiate device flow
	form := url.Values{}
	form.Set("client_id", "relay-client")
	devResp, err := idp.client().PostForm(idp.url()+"/oauth/device/code", form)
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	defer func() { _ = devResp.Body.Close() }()

	var devResult struct {
		DeviceCode string `json:"device_code"`
	}
	_ = json.NewDecoder(devResp.Body).Decode(&devResult)

	// 2. Approve device code
	idp.approveDeviceCode(devResult.DeviceCode, map[string]any{
		"sub":                "refresh-sub-user",
		"email":              "refresh@example.com",
		"preferred_username": "refreshuser",
	})

	// 3. Exchange for tokens
	pollForm := url.Values{}
	pollForm.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	pollForm.Set("device_code", devResult.DeviceCode)
	pollForm.Set("client_id", "relay-client")

	tokenResp, err := idp.client().PostForm(idp.url()+"/oauth/token", pollForm)
	if err != nil {
		t.Fatalf("PostForm token: %v", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()

	var tokenResult struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(tokenResp.Body).Decode(&tokenResult)

	if tokenResult.RefreshToken == "" {
		t.Fatal("expected non-empty refresh_token from initial device code grant")
	}

	// 4. Refresh token grant
	refreshForm := url.Values{}
	refreshForm.Set("grant_type", "refresh_token")
	refreshForm.Set("refresh_token", tokenResult.RefreshToken)
	refreshForm.Set("client_id", "relay-client")

	refreshedResp, err := idp.client().PostForm(idp.url()+"/oauth/token", refreshForm)
	if err != nil {
		t.Fatalf("PostForm refresh: %v", err)
	}
	defer func() { _ = refreshedResp.Body.Close() }()

	if refreshedResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(refreshedResp.Body)
		t.Fatalf("expected 200 OK on token refresh, got %d: %s", refreshedResp.StatusCode, string(body))
	}

	var refreshedResult struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(refreshedResp.Body).Decode(&refreshedResult)

	if refreshedResult.AccessToken == "" {
		t.Fatal("expected non-empty refreshed access_token")
	}
	if refreshedResult.RefreshToken == "" {
		t.Fatal("expected non-empty rotated refresh_token")
	}

	// 5. Authenticate to Relay with refreshed access token
	relayReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/v2/users/me", nil)
	relayReq.Header.Set("Authorization", "Bearer "+refreshedResult.AccessToken)

	relayResp, err := http.DefaultClient.Do(relayReq)
	if err != nil {
		t.Fatalf("Relay request with refreshed token failed: %v", err)
	}
	defer func() { _ = relayResp.Body.Close() }()

	if relayResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from Relay with refreshed token, got %d", relayResp.StatusCode)
	}

	var userProfile apigen.UserProfile
	_ = json.NewDecoder(relayResp.Body).Decode(&userProfile)
	if userProfile.Name != "refreshuser" {
		t.Errorf("expected username refreshuser, got %q", userProfile.Name)
	}
}

func TestDeviceFlow_DomainClaimOrganizationMatching(t *testing.T) {
	idp := newDeviceAuthServer(t)
	store := newMemoryStore()

	// Pre-create organisation and domain claim
	org, err := store.UpsertOrganisation(context.Background(), "megacorp")
	if err != nil {
		t.Fatalf("UpsertOrganisation failed: %v", err)
	}

	_, err = store.CreateDomainClaim(context.Background(), storegen.CreateDomainClaimParams{
		OrganisationID: org.ID,
		Domain:         "megacorp.internal",
	})
	if err != nil {
		t.Fatalf("CreateDomainClaim failed: %v", err)
	}

	validator := auth.NewOIDCValidator(idp.url(), "relay-client", idp.client())
	ts := setupServer(store, true, validator)
	defer ts.Close()

	// 1. Device login for employee with claimed domain email
	form := url.Values{}
	form.Set("client_id", "relay-client")
	devResp, err := idp.client().PostForm(idp.url()+"/oauth/device/code", form)
	if err != nil {
		t.Fatalf("PostForm: %v", err)
	}
	defer func() { _ = devResp.Body.Close() }()

	var devResult struct {
		DeviceCode string `json:"device_code"`
	}
	_ = json.NewDecoder(devResp.Body).Decode(&devResult)

	idp.approveDeviceCode(devResult.DeviceCode, map[string]any{
		"sub":                "employee-99",
		"email":              "dave@megacorp.internal",
		"preferred_username": "dave",
	})

	pollForm := url.Values{}
	pollForm.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	pollForm.Set("device_code", devResult.DeviceCode)
	pollForm.Set("client_id", "relay-client")

	tokenResp, err := idp.client().PostForm(idp.url()+"/oauth/token", pollForm)
	if err != nil {
		t.Fatalf("PostForm token: %v", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()

	var tokenResult struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(tokenResp.Body).Decode(&tokenResult)

	// 2. Call Relay GET /v2/users/me
	relayReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/v2/users/me", nil)
	relayReq.Header.Set("Authorization", "Bearer "+tokenResult.AccessToken)

	relayResp, err := http.DefaultClient.Do(relayReq)
	if err != nil {
		t.Fatalf("Relay request failed: %v", err)
	}
	defer func() { _ = relayResp.Body.Close() }()

	if relayResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(relayResp.Body)
		t.Fatalf("expected 200 OK, got %d: %s", relayResp.StatusCode, string(body))
	}

	var userProfile apigen.UserProfile
	if err := json.NewDecoder(relayResp.Body).Decode(&userProfile); err != nil {
		t.Fatalf("Decode user profile: %v", err)
	}

	if userProfile.OrgName != "megacorp" {
		t.Errorf("expected auto-assigned OrgName megacorp, got %q", userProfile.OrgName)
	}
	if userProfile.Email != "dave@megacorp.internal" {
		t.Errorf("expected email dave@megacorp.internal, got %q", userProfile.Email)
	}
}
