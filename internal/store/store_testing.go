//go:build !production

package store

import (
	"context"
)

// Truncate empties all application tables and reseeds global roles.
// It is intended for test environments only.
func (s *Store) Truncate(ctx context.Context) error {
	return s.b.Truncate(ctx)
}
