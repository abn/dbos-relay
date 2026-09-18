// Package config loads and validates the server's environment configuration.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Config is the fully validated server configuration.
type Config struct {
	// DatabaseURL points at Relay's database (PostgreSQL or SQLite). It is never
	// an application's system database.
	DatabaseURL string

	// Embedded indicates that Relay is running with an embedded SQLite engine.
	Embedded bool

	ListenAddr       string
	AdvertiseAddress string
	InternalSecret   string

	// OIDCIssuer, OIDCAudience, and OIDCClientID configure the identity provider.
	// When OIDCIssuer is set, authentication is enabled; when unset, Relay runs
	// in self-hosted no-auth mode.
	OIDCIssuer   string
	OIDCAudience string
	OIDCClientID string

	// ExecutorDeadline bounds every request dispatched to an executor.
	ExecutorDeadline time.Duration

	LogLevel slog.Level

	TLSCertFile string
	TLSKeyFile  string
	PeerScheme  string
}

// AuthEnabled reports whether OIDC authentication is configured.
func (c *Config) AuthEnabled() bool {
	return c.OIDCIssuer != ""
}

// IsEmbedded reports whether Relay is configured to run with an embedded SQLite engine.
func (c *Config) IsEmbedded() bool {
	if c.Embedded {
		return true
	}
	url := c.DatabaseURL
	return strings.HasPrefix(url, "sqlite://") ||
		strings.HasPrefix(url, "sqlite:") ||
		url == ":memory:" ||
		strings.HasSuffix(url, ".db") ||
		strings.HasSuffix(url, ".sqlite") ||
		strings.HasSuffix(url, ".sqlite3")
}

const (
	defaultListenAddr       = ":8090"
	defaultExecutorDeadline = 30 * time.Second
)

// Load reads configuration through getenv, applies defaults, and validates
// the result. It reports every problem it finds, not just the first.
func Load(getenv func(string) string) (*Config, error) {
	embeddedVal := strings.ToLower(getenv("RELAY_EMBEDDED"))
	isEmbedded := embeddedVal == "true" || embeddedVal == "1"
	dbURL := getenv("RELAY_DATABASE_URL")
	if dbURL == "" && isEmbedded {
		dbURL = "sqlite://./data/relay.db"
	}

	cfg := &Config{
		DatabaseURL:      dbURL,
		Embedded:         isEmbedded,
		ListenAddr:       or(getenv("RELAY_LISTEN_ADDR"), defaultListenAddr),
		// Provenance: https://docs.dbos.dev/production/hosting-conductor (confirmed 2026-09-08)
		AdvertiseAddress: or(or(getenv("RELAY_ADVERTISE_ADDRESS"), getenv("DBOS__ADVERTISE_ADDRESS")), "127.0.0.1"),
		InternalSecret:   getenv("RELAY_INTERNAL_SECRET"),
		OIDCIssuer:       getenv("RELAY_OIDC_ISSUER"),
		OIDCAudience:     getenv("RELAY_OIDC_AUDIENCE"),
		OIDCClientID:     getenv("RELAY_OIDC_CLIENT_ID"),
		ExecutorDeadline: defaultExecutorDeadline,
		LogLevel:         slog.LevelInfo,
		TLSCertFile:      getenv("RELAY_TLS_CERT_FILE"),
		TLSKeyFile:       getenv("RELAY_TLS_KEY_FILE"),
		PeerScheme:       or(getenv("RELAY_PEER_SCHEME"), "http"),
	}

	var problems []error

	if (cfg.TLSCertFile != "" && cfg.TLSKeyFile == "") || (cfg.TLSCertFile == "" && cfg.TLSKeyFile != "") {
		problems = append(problems, errors.New("both RELAY_TLS_CERT_FILE and RELAY_TLS_KEY_FILE must be set to enable TLS"))
	}

	if cfg.DatabaseURL == "" {
		problems = append(problems, errors.New("RELAY_DATABASE_URL is required"))
	}

	if cfg.OIDCIssuer != "" && cfg.OIDCAudience == "" {
		problems = append(problems, errors.New("RELAY_OIDC_AUDIENCE is required when RELAY_OIDC_ISSUER is set"))
	}

	if cfg.InternalSecret == "" && cfg.AuthEnabled() {
		problems = append(problems, errors.New("RELAY_INTERNAL_SECRET is required when authentication is enabled"))
	}

	if raw := getenv("RELAY_EXECUTOR_DEADLINE"); raw != "" {
		d, err := time.ParseDuration(raw)
		switch {
		case err != nil:
			problems = append(problems,
				fmt.Errorf("RELAY_EXECUTOR_DEADLINE: %w", err))
		case d <= 0:
			problems = append(problems,
				errors.New("RELAY_EXECUTOR_DEADLINE must be positive"))
		default:
			cfg.ExecutorDeadline = d
		}
	}

	if raw := getenv("RELAY_LOG_LEVEL"); raw != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(strings.ToUpper(raw))); err != nil {
			problems = append(problems, fmt.Errorf("RELAY_LOG_LEVEL: %w", err))
		}
	}

	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return cfg, nil
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
