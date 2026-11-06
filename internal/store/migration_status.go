package store

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// MigrationStatus describes the control-plane schema relative to the
// migrations embedded in this binary.
type MigrationStatus struct {
	// Version is the applied schema version, or zero when no migration has
	// been applied.
	Version uint
	// Dirty reports whether the migration runner left a failed migration.
	Dirty bool
	// Latest is the highest migration version embedded in this binary.
	Latest uint
	// DownAvailable reports whether a paired down migration is embedded for
	// Version.
	DownAvailable bool
}

// MigrationStatus reads the applied schema version and compares it with the
// migrations embedded in this binary. It never applies a migration.
func (s *Store) MigrationStatus(ctx context.Context) (MigrationStatus, error) {
	v, dirty, err := s.MigrateVersion(ctx)
	if err != nil {
		return MigrationStatus{}, err
	}

	fsys, dir := migrationFS, "migrations"
	if isSQLiteURL(s.url) {
		fsys, dir = sqliteMigrationFS, "migrations_sqlite"
	}
	latest, down, err := embeddedMigrations(fsys, dir)
	if err != nil {
		return MigrationStatus{}, err
	}

	return MigrationStatus{
		Version:       v,
		Dirty:         dirty,
		Latest:        latest,
		DownAvailable: v > 0 && down[v],
	}, nil
}

// embeddedMigrations returns the highest up-migration version and the set of
// versions that have a paired down migration.
func embeddedMigrations(fsys fs.FS, dir string) (uint, map[uint]bool, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return 0, nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	var latest uint
	down := make(map[uint]bool)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		versionPart, _, ok := strings.Cut(name, "_")
		if !ok {
			return 0, nil, fmt.Errorf("migration %q has no version prefix", name)
		}
		version, err := strconv.ParseUint(versionPart, 10, 32)
		if err != nil {
			return 0, nil, fmt.Errorf("migration %q has an invalid version prefix: %w", name, err)
		}
		if strings.HasSuffix(name, ".up.sql") && uint(version) > latest {
			latest = uint(version)
		}
		if strings.HasSuffix(name, ".down.sql") {
			down[uint(version)] = true
		}
	}
	if latest == 0 {
		return 0, nil, fmt.Errorf("no embedded up migrations found in %s", dir)
	}
	return latest, down, nil
}
