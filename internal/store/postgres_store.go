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

type postgresBackend struct {
	pool *pgxpool.Pool
	url  string
	q    *gen.Queries
}

var _ backend = (*postgresBackend)(nil)

func openPostgres(ctx context.Context, url string) (*postgresBackend, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database url: %w", err)
	}

	if cfg.MaxConns <= 0 || cfg.MaxConns > 10 {
		cfg.MaxConns = 10
	}
	if cfg.MinConns < 1 {
		cfg.MinConns = 1
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &postgresBackend{
		pool: pool,
		url:  url,
		q:    gen.New(pool),
	}, nil
}

func (b *postgresBackend) Queries() gen.Querier {
	return b.q
}

func (b *postgresBackend) InTx(ctx context.Context, fn func(gen.Querier) error) error {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	qtx := b.q.WithTx(tx)
	if err := fn(qtx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (b *postgresBackend) Ping(ctx context.Context) error {
	return b.pool.Ping(ctx)
}

func (b *postgresBackend) newMigrate() (*migrate.Migrate, error) {
	d, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to create migration source: %w", err)
	}

	migrateURL := b.url
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

func (b *postgresBackend) Migrate(ctx context.Context) error {
	m, err := b.newMigrate()
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

func (b *postgresBackend) MigrateVersion(ctx context.Context) (uint, bool, error) {
	m, err := b.newMigrate()
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

func (b *postgresBackend) MigrateForce(ctx context.Context, version int) error {
	m, err := b.newMigrate()
	if err != nil {
		return err
	}
	defer func() {
		_, _ = m.Close()
	}()

	return m.Force(version)
}

func (b *postgresBackend) MigrateDown(ctx context.Context, steps int) error {
	m, err := b.newMigrate()
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

func (b *postgresBackend) Close() {
	if b.pool != nil {
		b.pool.Close()
	}
}

func (b *postgresBackend) Truncate(ctx context.Context) error {
	tables := []string{
		"audit_logs",
		"recovery_dispatches",
		"domain_claims",
		"organisation_members",
		"users",
		"alerting_rules",
		"api_keys",
		"executors",
		"instances",
		"applications",
		"organisations",
	}

	query := fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", strings.Join(tables, ", "))

	_, err := b.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to truncate tables: %w", err)
	}

	if _, err := b.pool.Exec(ctx, "DELETE FROM roles WHERE organisation_id IS NOT NULL"); err != nil {
		return fmt.Errorf("failed to clean custom roles: %w", err)
	}

	reseed := `
		INSERT INTO roles (organisation_id, name, permissions, is_global)
		VALUES
			(NULL, 'admin', ARRAY['application.read', 'application.write', 'websocket.connect'], true),
			(NULL, 'operator', ARRAY['application.read', 'application.write', 'websocket.connect'], true),
			(NULL, 'viewer', ARRAY['application.read'], true)
		ON CONFLICT DO NOTHING;
	`
	if _, err := b.pool.Exec(ctx, reseed); err != nil {
		return fmt.Errorf("failed to re-seed global roles: %w", err)
	}

	return nil
}
