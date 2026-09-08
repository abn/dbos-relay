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
	// DatabaseURL points at Relay's own Postgres database. It is never an
	// application's system database.
	DatabaseURL string

	ListenAddr       string
	AdvertiseAddress string
	InternalSecret   string

	// ExecutorDeadline bounds every request dispatched to an executor.
	ExecutorDeadline time.Duration

	LogLevel slog.Level
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
		AdvertiseAddress: getenv("RELAY_ADVERTISE_ADDRESS"),
		InternalSecret:   getenv("RELAY_INTERNAL_SECRET"),
		ExecutorDeadline: defaultExecutorDeadline,
		LogLevel:         slog.LevelInfo,
	}

	var problems []error

	if cfg.DatabaseURL == "" {
		problems = append(problems, errors.New("RELAY_DATABASE_URL is required"))
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
