// Package config loads and validates the server's environment configuration.
package config

import (
	"errors"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Config is the fully validated server configuration.
type Config struct {
	// DatabaseURL points at Relay's own Postgres database. It is never an
	// application's system database.
	DatabaseURL string

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
}

// AuthEnabled reports whether OIDC authentication is configured.
func (c *Config) AuthEnabled() bool {
	return c.OIDCIssuer != ""
}

const (
	defaultListenAddr       = ":8090"
	defaultExecutorDeadline = 30 * time.Second
)

// Load reads configuration through getenv, applies defaults, and validates
// the result. It reports every problem it finds, not just the first.
func Load(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		DatabaseURL:      getenv("RELAY_DATABASE_URL"),
		ListenAddr:       or(getenv("RELAY_LISTEN_ADDR"), defaultListenAddr),
		AdvertiseAddress: or(or(getenv("RELAY_ADVERTISE_ADDRESS"), getenv("DBOS__ADVERTISE_ADDRESS")), "127.0.0.1"),
		InternalSecret:   or(getenv("RELAY_INTERNAL_SECRET"), getenv("DBOS__CLUSTER_SECRET")),
		OIDCIssuer:       getenv("RELAY_OIDC_ISSUER"),
		OIDCAudience:     getenv("RELAY_OIDC_AUDIENCE"),
		OIDCClientID:     getenv("RELAY_OIDC_CLIENT_ID"),
		ExecutorDeadline: defaultExecutorDeadline,
		LogLevel:         slog.LevelInfo,
	}

	var problems []error

	if cfg.DatabaseURL == "" {

	if cfg.OIDCIssuer != "" && cfg.OIDCAudience == "" {
		problems = append(problems, errors.New("RELAY_OIDC_AUDIENCE is required when RELAY_OIDC_ISSUER is set"))
	}
		problems = append(problems, errors.New("RELAY_DATABASE_URL is required"))

	if cfg.OIDCIssuer != "" && cfg.OIDCAudience == "" {
		problems = append(problems, errors.New("RELAY_OIDC_AUDIENCE is required when RELAY_OIDC_ISSUER is set"))
	}
	}

	if cfg.OIDCIssuer != "" && cfg.OIDCAudience == "" {
		problems = append(problems, errors.New("RELAY_OIDC_AUDIENCE is required when RELAY_OIDC_ISSUER is set"))
	}

	if cfg.InternalSecret == "" {
		if cfg.AuthEnabled() {
			problems = append(problems, errors.New("RELAY_INTERNAL_SECRET is required when authentication is enabled"))
		} else {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err == nil {
				cfg.InternalSecret = base64.RawURLEncoding.EncodeToString(raw)
			}
		}
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
