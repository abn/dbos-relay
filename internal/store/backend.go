package store

import (
	"context"

	"github.com/abn/relay/internal/store/gen"
)

type backend interface {
	Queries() gen.Querier
	InTx(ctx context.Context, fn func(gen.Querier) error) error
	Ping(ctx context.Context) error
	Migrate(ctx context.Context) error
	MigrateVersion(ctx context.Context) (uint, bool, error)
	MigrateForce(ctx context.Context, version int) error
	MigrateDown(ctx context.Context, steps int) error
	Close()
	Truncate(ctx context.Context) error
}
