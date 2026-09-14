package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/abn/relay/internal/config"
)

func env(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := config.Load(env(nil))
	if err == nil {
		t.Fatal("want an error when RELAY_DATABASE_URL is unset, got nil")
	}
	if !strings.Contains(err.Error(), "RELAY_DATABASE_URL") {
		t.Fatalf("error should name the missing variable, got %q", err)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	got, err := config.Load(env(map[string]string{
		"RELAY_DATABASE_URL": "postgres://localhost/relay",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ListenAddr != ":8090" {
		t.Errorf("ListenAddr = %q, want \":8090\"", got.ListenAddr)
	}
	if got.ExecutorDeadline != 30*time.Second {
		t.Errorf("ExecutorDeadline = %v, want 30s", got.ExecutorDeadline)
	}
}

func TestLoadRejectsUnparsableDeadline(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"RELAY_DATABASE_URL":      "postgres://localhost/relay",
		"RELAY_EXECUTOR_DEADLINE": "half a minute",
	}))
	if err == nil {
		t.Fatal("want an error for an unparsable duration, got nil")
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"RELAY_EXECUTOR_DEADLINE": "nonsense",
	}))
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "RELAY_DATABASE_URL") ||
		!strings.Contains(msg, "RELAY_EXECUTOR_DEADLINE") {
		t.Fatalf("want both problems reported, got %q", msg)
	}
}

func TestLoadInternalSecretUnset(t *testing.T) {
	got, err := config.Load(env(map[string]string{
		"RELAY_DATABASE_URL": "postgres://localhost/relay",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.InternalSecret != "" {
		t.Errorf("InternalSecret = %q, want empty string when unset", got.InternalSecret)
	}

	// When auth is enabled, RELAY_INTERNAL_SECRET is required.
	_, err = config.Load(env(map[string]string{
		"RELAY_DATABASE_URL":  "postgres://localhost/relay",
		"RELAY_OIDC_ISSUER":   "https://issuer.example.com",
		"RELAY_OIDC_AUDIENCE": "aud",
	}))
	if err == nil {
		t.Fatal("want error when auth enabled but internal secret unset")
	}
	if !strings.Contains(err.Error(), "RELAY_INTERNAL_SECRET") {
		t.Fatalf("error should mention RELAY_INTERNAL_SECRET, got %q", err)
	}
}

func TestLoadOIDCRequiresAudience(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"RELAY_DATABASE_URL": "postgres://localhost/relay",
		"RELAY_OIDC_ISSUER":  "https://issuer.example.com",
	}))
	if err == nil {
		t.Fatal("want error when RELAY_OIDC_ISSUER is set without RELAY_OIDC_AUDIENCE")
	}
	if !strings.Contains(err.Error(), "RELAY_OIDC_AUDIENCE") {
		t.Fatalf("error should name missing RELAY_OIDC_AUDIENCE, got %q", err)
	}
}

func TestLoadTLSConfiguration(t *testing.T) {
	t.Run("both cert and key set", func(t *testing.T) {
		got, err := config.Load(env(map[string]string{
			"RELAY_DATABASE_URL":   "postgres://localhost/relay",
			"RELAY_TLS_CERT_FILE":  "/path/to/cert.pem",
			"RELAY_TLS_KEY_FILE":   "/path/to/key.pem",
			"RELAY_PEER_SCHEME":    "https",
		}))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.TLSCertFile != "/path/to/cert.pem" {
			t.Errorf("expected TLSCertFile %q, got %q", "/path/to/cert.pem", got.TLSCertFile)
		}
		if got.TLSKeyFile != "/path/to/key.pem" {
			t.Errorf("expected TLSKeyFile %q, got %q", "/path/to/key.pem", got.TLSKeyFile)
		}
		if got.PeerScheme != "https" {
			t.Errorf("expected PeerScheme https, got %q", got.PeerScheme)
		}
	})

	t.Run("only cert set fails", func(t *testing.T) {
		_, err := config.Load(env(map[string]string{
			"RELAY_DATABASE_URL":  "postgres://localhost/relay",
			"RELAY_TLS_CERT_FILE": "/path/to/cert.pem",
		}))
		if err == nil {
			t.Fatal("expected error when only cert file is set, got nil")
		}
		if !strings.Contains(err.Error(), "both RELAY_TLS_CERT_FILE and RELAY_TLS_KEY_FILE must be set") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("only key set fails", func(t *testing.T) {
		_, err := config.Load(env(map[string]string{
			"RELAY_DATABASE_URL": "postgres://localhost/relay",
			"RELAY_TLS_KEY_FILE":  "/path/to/key.pem",
		}))
		if err == nil {
			t.Fatal("expected error when only key file is set, got nil")
		}
		if !strings.Contains(err.Error(), "both RELAY_TLS_CERT_FILE and RELAY_TLS_KEY_FILE must be set") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
