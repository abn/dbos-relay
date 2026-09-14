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

func (s *Store) InTx(ctx context.Context, fn func(*gen.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	qtx := s.q.WithTx(tx)
	if err := fn(qtx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) newMigrate() (*migrate.Migrate, error) {
	d, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to create migration source: %w", err)
	}

	migrateURL := s.url
	if strings.HasPrefix(migrateURL, "postgres://") {
		migrateURL = "pgx5://" + strings.TrimPrefix(migrateURL, "postgres://")
	} else if strings.HasPrefix(migrateURL, "postgresql://") {
		migrateURL = "pgx5://" + strings.TrimPrefix(migrateURL, "postgresql://")
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, migrateURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create migrate instance: %w", err)
	}
	return m, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	m, err := s.newMigrate()
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		if strings.Contains(err.Error(), "Dirty database") {
			v, _, vErr := m.Version()
			if vErr == nil {
				return fmt.Errorf("database schema is dirty at version %d: resolve conflicts and run 'relay migrate force --version %d --confirm'", v, v)
			}
			return fmt.Errorf("database schema is dirty: resolve conflicts and run 'relay migrate force --version <version> --confirm' (%w)", err)
		}
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func (s *Store) MigrateVersion(ctx context.Context) (uint, bool, error) {
	m, err := s.newMigrate()
	if err != nil {
		return 0, false, err
	}
	defer func() {
		_, _ = m.Close()
	}()

	v, dirty, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return v, dirty, nil
}

func (s *Store) MigrateForce(ctx context.Context, version int) error {
	m, err := s.newMigrate()
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.Close()
	}()

	return m.Force(version)
}

func (s *Store) MigrateDown(ctx context.Context, steps int) error {
	m, err := s.newMigrate()
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.Close()
	}()

	if steps <= 0 {
		steps = 1
	}
	err = m.Steps(-steps)
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}
