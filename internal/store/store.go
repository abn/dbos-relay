package store

import (
	"context"
	"strings"

	"github.com/abn/relay/internal/store/gen"
)

type Store struct {
	b   backend
	url string
}

func Open(ctx context.Context, url string) (*Store, error) {
	if isSQLiteURL(url) {
		b, err := openSQLite(ctx, url)
		if err != nil {
			return nil, err
		}
		return &Store{b: b, url: url}, nil
	}

	b, err := openPostgres(ctx, url)
	if err != nil {
		return nil, err
	}
	return &Store{b: b, url: url}, nil
}

func isSQLiteURL(url string) bool {
	if strings.HasPrefix(url, "sqlite://") || strings.HasPrefix(url, "sqlite:") {
		return true
	}
	if url == ":memory:" {
		return true
	}
	if strings.HasSuffix(url, ".db") || strings.HasSuffix(url, ".sqlite") || strings.HasSuffix(url, ".sqlite3") {
		return true
	}
	return false
}

func (s *Store) Queries() gen.Querier {
	return s.b.Queries()
}

func (s *Store) InTx(ctx context.Context, fn func(gen.Querier) error) error {
	return s.b.InTx(ctx, fn)
}

func (s *Store) Ping(ctx context.Context) error {
	return s.b.Ping(ctx)
}

func (s *Store) Migrate(ctx context.Context) error {
	return s.b.Migrate(ctx)
}

func (s *Store) MigrateVersion(ctx context.Context) (uint, bool, error) {
	return s.b.MigrateVersion(ctx)
}

func (s *Store) MigrateForce(ctx context.Context, version int) error {
	return s.b.MigrateForce(ctx, version)
}

func (s *Store) MigrateDown(ctx context.Context, steps int) error {
	return s.b.MigrateDown(ctx, steps)
}

func (s *Store) Close() {
	if s.b != nil {
		s.b.Close()
	}
}
