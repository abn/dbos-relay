package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abn/relay/internal/store/gen"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	pool *pgxpool.Pool
	url  string
	q    *gen.Queries
}

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Store{
		pool: pool,
		url:  url,
		q:    gen.New(pool),
	}, nil
}

func (s *Store) Queries() *gen.Queries {
	return s.q
}

func (s *Store) Migrate(ctx context.Context) error {
	d, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, s.url)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func (s *Store) Truncate(ctx context.Context) error {
	// Truncate tables in dependency order (reverse of creation)
	tables := []string{
		"api_keys",
		"executors",
		"instances",
		"applications",
		"organisations",
	}

	// We can use a single TRUNCATE statement with CASCADE to handle dependencies,
	// but the instructions say "emptying tables in dependency order".
	query := fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", strings.Join(tables, ", "))

	_, err := s.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to truncate tables: %w", err)
	}
	return nil
}

func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}
