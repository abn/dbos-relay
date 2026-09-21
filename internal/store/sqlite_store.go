package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	sqliteMigrate "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"

	"github.com/abn/relay/internal/store/gen"
)

//go:embed migrations_sqlite/*.sql
var sqliteMigrationFS embed.FS

type sqliteBackend struct {
	db  *sql.DB
	dsn string
	q   *sqliteQueries
}

var _ backend = (*sqliteBackend)(nil)

func openSQLite(ctx context.Context, rawURL string) (*sqliteBackend, error) {
	dsn := rawURL
	if strings.HasPrefix(dsn, "sqlite://") {
		dsn = strings.TrimPrefix(dsn, "sqlite://")
	} else if strings.HasPrefix(dsn, "sqlite:") {
		dsn = strings.TrimPrefix(dsn, "sqlite:")
	}

	isMemory := dsn == ":memory:" || strings.Contains(dsn, "mode=memory") || strings.Contains(dsn, ":memory:")

	var fullDSN string
	if isMemory {
		if strings.Contains(dsn, "?") {
			fullDSN = dsn + "&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
		} else {
			fullDSN = dsn + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
		}
	} else {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		fullDSN = dsn + sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}

	db, err := sql.Open("sqlite", fullDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	if isMemory {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	return &sqliteBackend{
		db:  db,
		dsn: fullDSN,
		q:   newSQLiteQueries(db),
	}, nil
}

func (b *sqliteBackend) Queries() gen.Querier {
	return b.q
}

func (b *sqliteBackend) InTx(ctx context.Context, fn func(gen.Querier) error) error {
	tx, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin sqlite transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	qtx := newSQLiteQueries(tx)
	if err := fn(qtx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit sqlite transaction: %w", err)
	}

	return nil
}

func (b *sqliteBackend) Ping(ctx context.Context) error {
	return b.db.PingContext(ctx)
}

func (b *sqliteBackend) newMigrate() (*migrate.Migrate, error) {
	d, err := iofs.New(sqliteMigrationFS, "migrations_sqlite")
	if err != nil {
		return nil, fmt.Errorf("failed to create sqlite migration source: %w", err)
	}

	driver, err := sqliteMigrate.WithInstance(b.db, &sqliteMigrate.Config{
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sqlite migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", d, "sqlite", driver)
	if err != nil {
		return nil, fmt.Errorf("failed to create sqlite migrate instance: %w", err)
	}
	return m, nil
}

func (b *sqliteBackend) Migrate(ctx context.Context) error {
	m, err := b.newMigrate()
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		if strings.Contains(err.Error(), "Dirty database") {
			v, _, vErr := m.Version()
			if vErr == nil {
				return fmt.Errorf("database schema is dirty at version %d: resolve conflicts and run 'relay migrate force --version %d --confirm'", v, v)
			}
			return fmt.Errorf("database schema is dirty: resolve conflicts and run 'relay migrate force --version <version> --confirm' (%w)", err)
		}
		return fmt.Errorf("failed to run sqlite migrations: %w", err)
	}

	return nil
}

func (b *sqliteBackend) MigrateVersion(ctx context.Context) (uint, bool, error) {
	m, err := b.newMigrate()
	if err != nil {
		return 0, false, err
	}

	v, dirty, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return v, dirty, nil
}

func (b *sqliteBackend) MigrateForce(ctx context.Context, version int) error {
	m, err := b.newMigrate()
	if err != nil {
		return err
	}

	return m.Force(version)
}

func (b *sqliteBackend) MigrateDown(ctx context.Context, steps int) error {
	m, err := b.newMigrate()
	if err != nil {
		return err
	}

	if steps <= 0 {
		steps = 1
	}
	err = m.Steps(-steps)
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

func (b *sqliteBackend) Close() {
	if b.db != nil {
		_ = b.db.Close()
	}
}

func (b *sqliteBackend) Truncate(ctx context.Context) error {
	tables := []string{
		"audit_logs",
		"recovery_dispatches",
		"domain_claims",
		"organisation_members",
		"users",
		"alerting_rules",
		"autoscaling_policies",
		"api_keys",
		"executors",
		"instances",
		"applications",
		"organisations",
	}

	if _, err := b.db.ExecContext(ctx, "PRAGMA foreign_keys = OFF;"); err != nil {
		return fmt.Errorf("failed to disable foreign keys: %w", err)
	}
	defer func() {
		_, _ = b.db.ExecContext(ctx, "PRAGMA foreign_keys = ON;")
	}()

	for _, table := range tables {
		if _, err := b.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s;", table)); err != nil {
			return fmt.Errorf("failed to delete from %s: %w", table, err)
		}
	}

	if _, err := b.db.ExecContext(ctx, "DELETE FROM roles WHERE organisation_id IS NOT NULL;"); err != nil {
		return fmt.Errorf("failed to clean custom roles: %w", err)
	}

	reseed := `
		INSERT INTO roles (organisation_id, name, permissions, is_global)
		VALUES
			(NULL, 'admin', '["application.read", "application.write", "websocket.connect", "metric.read", "organization.read", "organization.write", "token.read", "token.write"]', 1),
			(NULL, 'operator', '["application.read", "application.write", "websocket.connect", "metric.read", "organization.read", "organization.write", "token.read", "token.write"]', 1),
			(NULL, 'viewer', '["application.read", "metric.read", "organization.read", "token.read"]', 1)
		ON CONFLICT DO NOTHING;
	`
	if _, err := b.db.ExecContext(ctx, reseed); err != nil {
		return fmt.Errorf("failed to re-seed global roles: %w", err)
	}

	return nil
}
