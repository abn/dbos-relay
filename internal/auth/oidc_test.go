package auth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abn/relay/internal/auth"
)

type mockOIDCServer struct {
	privKey *rsa.PrivateKey
	kid     string
	server  *httptest.Server
}

func newMockOIDCServer(t *testing.T) *mockOIDCServer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	kid := "test-key-1"
	m := &mockOIDCServer{privKey: priv, kid: kid}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":   base,
			"jwks_uri": base + "/jwks.json",
		})
	})

	mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, r *http.Request) {
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

	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockOIDCServer) mintToken(t *testing.T, claims map[string]any) string {
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

func TestOIDCValidator_ValidToken(t *testing.T) {
	mock := newMockOIDCServer(t)
	validator := auth.NewOIDCValidator(mock.server.URL, "test-audience", mock.server.Client())

	token := mock.mintToken(t, map[string]any{
		"sub":                "user-123",
		"email":              "user@example.com",
		"preferred_username": "alice",
		"iss":                mock.server.URL,
		"aud":                "test-audience",
		"exp":                time.Now().Add(time.Hour).Unix(),
	})

	claims, err := validator.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if claims.Subject != "user-123" || claims.Email != "user@example.com" || claims.PreferredUsername != "alice" {
		t.Errorf("unexpected claims: %+v", claims)
	}
}

func TestOIDCValidator_ArrayAudience(t *testing.T) {
	mock := newMockOIDCServer(t)
	validator := auth.NewOIDCValidator(mock.server.URL, "test-audience", mock.server.Client())

	token := mock.mintToken(t, map[string]any{
		"sub":  "user-123",
		"iss":  mock.server.URL,
		"aud":  []string{"other-aud", "test-audience"},
		"exp":  time.Now().Add(time.Hour).Unix(),
	})

	claims, err := validator.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if claims.Subject != "user-123" {
		t.Errorf("subject = %q, want user-123", claims.Subject)
	}
}

func TestOIDCValidator_FailClosedOnUnavailableJWKS(t *testing.T) {
	// Point to invalid port that refuses connections
	validator := auth.NewOIDCValidator("http://127.0.0.1:59999", "test-audience", nil)

	// Attempting to validate should fail closed with ErrKeySetUnavailable
	_, err := validator.Validate(context.Background(), "eyJhbGciOiJSUzI1NiIsImtpZCI6IjEifQ.eyJzdWIiOiIxIn0.c2ln")
	if err == nil {
		t.Fatal("expected validation to fail closed when JWKS is unreachable")
	}
}

func TestOIDCValidator_RejectsMismatchedIssuer(t *testing.T) {
	mock := newMockOIDCServer(t)
	validator := auth.NewOIDCValidator(mock.server.URL, "test-audience", mock.server.Client())

	token := mock.mintToken(t, map[string]any{
		"sub": "user-123",
		"iss": "http://malicious-issuer.com",
		"aud": "test-audience",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err := validator.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error on mismatched issuer")
	}
}

func TestOIDCValidator_RejectsExpiredToken(t *testing.T) {
	mock := newMockOIDCServer(t)
	validator := auth.NewOIDCValidator(mock.server.URL, "test-audience", mock.server.Client())

	token := mock.mintToken(t, map[string]any{
		"sub": "user-123",
		"iss": mock.server.URL,
		"aud": "test-audience",
		"exp": time.Now().Add(-1 * time.Minute).Unix(),
	})

	_, err := validator.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error on expired token")
	}
}

func TestOIDCValidator_RejectsTamperedSignature(t *testing.T) {
	mock := newMockOIDCServer(t)
	validator := auth.NewOIDCValidator(mock.server.URL, "test-audience", mock.server.Client())

	token := mock.mintToken(t, map[string]any{
		"sub": "user-123",
		"iss": mock.server.URL,
		"aud": "test-audience",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	// Tamper payload
	tampered := token[:len(token)-5] + "XXXXX"
	_, err := validator.Validate(context.Background(), tampered)
	if err == nil {
		t.Fatal("expected error on tampered signature")
	}
}
