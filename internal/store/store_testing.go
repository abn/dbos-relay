//go:build !production

package store

import (
	"context"
	"fmt"
	"strings"
)

// Truncate empties all application tables and reseeds global roles.
// It is intended for test environments only.
func (s *Store) Truncate(ctx context.Context) error {
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

	_, err := s.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to truncate tables: %w", err)
	}

	if _, err := s.pool.Exec(ctx, "DELETE FROM roles WHERE organisation_id IS NOT NULL"); err != nil {
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
	if _, err := s.pool.Exec(ctx, reseed); err != nil {
		return fmt.Errorf("failed to re-seed global roles: %w", err)
	}

	return nil
}
