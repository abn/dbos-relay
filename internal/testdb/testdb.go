package testdb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryLockKey int64 = 74839201948271

var (
	dbsMu      sync.Mutex
	createdDBs = make(map[string]string)
)

// URL returns a connection string pointing to an isolated per-package database.
// If RELAY_TEST_DATABASE_URL is not set, it returns an empty string without error.
// It connects to the maintenance database specified in the base URL and creates
// the target package database if it does not already exist.
func URL(pkgName string) (string, error) {
	raw := os.Getenv("RELAY_TEST_DATABASE_URL")
	if raw == "" {
		return "", nil
	}

	dbsMu.Lock()
	defer dbsMu.Unlock()

	if targetURL, ok := createdDBs[pkgName]; ok {
		return targetURL, nil
	}

	baseParsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing RELAY_TEST_DATABASE_URL: %w", err)
	}

	dbName := sanitizeDBName(pkgName)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, raw)
	if err != nil {
		fallbackURL := *baseParsed
		fallbackURL.Path = "/postgres"
		var fallbackErr error
		conn, fallbackErr = pgx.Connect(ctx, fallbackURL.String())
		if fallbackErr != nil {
			return "", fmt.Errorf("connecting to maintenance database (%s): %w (fallback /postgres failed: %s)", raw, err, fallbackErr.Error())
		}
	}
	defer func() {
		_ = conn.Close(context.Background())
	}()

	query := fmt.Sprintf(`CREATE DATABASE "%s"`, dbName)
	if _, err := conn.Exec(ctx, query); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P04" {
			// duplicate_database: already created by a concurrent runner
		} else if strings.Contains(err.Error(), "42P04") || strings.Contains(strings.ToLower(err.Error()), "already exists") {
			// duplicate database error tolerated
		} else {
			return "", fmt.Errorf("creating database %q: %w", dbName, err)
		}
	}

	targetParsed := *baseParsed
	targetParsed.Path = "/" + dbName
	targetString := targetParsed.String()

	createdDBs[pkgName] = targetString
	return targetString, nil
}

// OpenStore provisions an isolated test database for pkgName and returns its connection URL.
// It calls t.Skip if RELAY_TEST_DATABASE_URL is unset.
func OpenStore(t testing.TB, pkgName string) string {
	t.Helper()

	pkgURL, err := URL(pkgName)
	if err != nil {
		t.Fatalf("failed to derive test database url for %s: %v", pkgName, err)
	}
	if pkgURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	return pkgURL
}

// AcquireLock acquires a dedicated connection from pool and takes a session-level
// advisory lock on a fixed key. The returned unlock function releases the lock and
// returns the dedicated connection to the pool.
func AcquireLock(ctx context.Context, pool *pgxpool.Pool) (func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection for advisory lock: %w", err)
	}

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		conn.Release()
		return nil, fmt.Errorf("acquiring pg_advisory_lock: %w", err)
	}

	var once sync.Once
	unlock := func() {
		once.Do(func() {
			_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", advisoryLockKey)
			conn.Release()
		})
	}

	return unlock, nil
}

func sanitizeDBName(pkgName string) string {
	var sb strings.Builder
	for _, r := range pkgName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return "relay_test_" + sb.String()
}
