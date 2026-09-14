package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
	"github.com/abn/relay/internal/testdb"
)

func TestSchemaHygiene_NoUnindexedForeignKeys(t *testing.T) {
	dbURL := testdb.OpenStore(t, "schema_hygiene_fk")
	ctx := context.Background()

	s, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	query := `
		SELECT c.conrelid::regclass::text, c.conname, a.attname
		FROM pg_constraint c
		JOIN LATERAL unnest(c.conkey) k(attnum) ON true
		JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum
		WHERE c.contype='f' AND NOT EXISTS (
			SELECT 1 FROM pg_index i
			WHERE i.indrelid=c.conrelid AND i.indkey[0]=c.conkey[1]
		);
	`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		t.Fatalf("failed to query unindexed foreign keys: %v", err)
	}
	defer rows.Close()

	type unindexedFK struct {
		rel  string
		name string
		att  string
	}
	var missing []unindexedFK
	for rows.Next() {
		var m unindexedFK
		if err := rows.Scan(&m.rel, &m.name, &m.att); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
		missing = append(missing, m)
	}

	if len(missing) > 0 {
		t.Errorf("expected 0 unindexed foreign keys, got %d: %+v", len(missing), missing)
	}
}

func TestSchemaHygiene_RoleReferentialIntegrity(t *testing.T) {
	dbURL := testdb.OpenStore(t, "schema_hygiene_roles")
	ctx := context.Background()

	s, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	orgName := "test_roles_org_" + uuid.NewString()[:8]
	org, err := s.Queries().CreateOrganisation(ctx, orgName)
	if err != nil {
		t.Fatalf("failed to create organisation: %v", err)
	}

	user, err := s.Queries().CreateUser(ctx, gen.CreateUserParams{
		Subject:  "subject_" + uuid.NewString(),
		Username: "user_" + uuid.NewString()[:8],
		Email:    "user@example.com",
		IsAdmin:  false,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	// 1. Inserting non-existent role should fail
	_, err = s.Queries().UpsertMemberRole(ctx, gen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		RoleName:       "does_not_exist",
	})
	if err == nil {
		t.Fatalf("expected error when granting non-existent role, got nil")
	}

	// 2. Global roles (admin, operator, viewer) should succeed
	for _, role := range []string{"admin", "operator", "viewer"} {
		_, err = s.Queries().UpsertMemberRole(ctx, gen.UpsertMemberRoleParams{
			OrganisationID: org.ID,
			UserID:         user.ID,
			RoleName:       role,
		})
		if err != nil {
			t.Fatalf("expected granting global role %q to succeed, got %v", role, err)
		}
	}

	// 3. Create a custom role in this organisation
	customRole, err := s.Queries().CreateRole(ctx, gen.CreateRoleParams{
		OrganisationID: org.ID,
		Name:           "custom_role",
		Permissions:    []string{"application.read"},
	})
	if err != nil {
		t.Fatalf("failed to create custom role: %v", err)
	}

	// Assign custom role to member
	_, err = s.Queries().UpsertMemberRole(ctx, gen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		RoleName:       customRole.Name,
	})
	if err != nil {
		t.Fatalf("failed to grant custom role: %v", err)
	}

	// 4. Deleting custom role while member references it should be rejected
	_, err = s.Queries().DeleteRole(ctx, gen.DeleteRoleParams{
		OrganisationID: org.ID,
		Name:           customRole.Name,
	})
	if err == nil {
		t.Fatalf("expected deleting role referenced by member to fail, got nil")
	}

	// 5. Reassign member to global role, then delete custom role should succeed
	_, err = s.Queries().UpsertMemberRole(ctx, gen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		RoleName:       "viewer",
	})
	if err != nil {
		t.Fatalf("failed to reassign member to viewer: %v", err)
	}

	_, err = s.Queries().DeleteRole(ctx, gen.DeleteRoleParams{
		OrganisationID: org.ID,
		Name:           customRole.Name,
	})
	if err != nil {
		t.Fatalf("expected deleting unreferenced custom role to succeed, got %v", err)
	}
}

func TestSchemaHygiene_TouchAPIKeyLastUsed(t *testing.T) {
	dbURL := testdb.OpenStore(t, "schema_hygiene_apikey")
	ctx := context.Background()

	s, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	orgName := "test_apikey_org_" + uuid.NewString()[:8]
	org, err := s.Queries().CreateOrganisation(ctx, orgName)
	if err != nil {
		t.Fatalf("failed to create organisation: %v", err)
	}

	keyName := "test_key_" + uuid.NewString()[:8]
	keyRecord, err := s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             keyName,
		Lookup:           "lookup_" + keyName,
		KeyHash:          []byte("hash"),
		ApplicationNames: []string{},
		Permissions:      []string{"application.read"},
	})
	if err != nil {
		t.Fatalf("failed to create api key: %v", err)
	}

	// Check initially last_used_at is not valid / null
	if keyRecord.LastUsedAt.Valid {
		t.Fatalf("expected initially last_used_at to be NULL, got %v", keyRecord.LastUsedAt.Time)
	}

	// Call TouchAPIKeyLastUsed
	before := time.Now().Add(-1 * time.Second)
	if err := s.Queries().TouchAPIKeyLastUsed(ctx, keyRecord.ID); err != nil {
		t.Fatalf("failed to touch api key: %v", err)
	}
	after := time.Now().Add(1 * time.Second)

	// Fetch key by ID and verify last_used_at is non-null and within the last minute
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var lastUsedAt pgtype.Timestamptz
	err = conn.QueryRow(ctx, "SELECT last_used_at FROM api_keys WHERE name = $1", keyName).Scan(&lastUsedAt)
	if err != nil {
		t.Fatalf("failed to query last_used_at: %v", err)
	}

	if !lastUsedAt.Valid {
		t.Fatalf("expected last_used_at to be non-NULL after touch")
	}
	if lastUsedAt.Time.Before(before) || lastUsedAt.Time.After(after) {
		t.Fatalf("last_used_at %v is outside expected window [%v, %v]", lastUsedAt.Time, before, after)
	}
}

func TestSchemaHygiene_OIDCUsernameNormalization(t *testing.T) {
	dbURL := testdb.OpenStore(t, "schema_hygiene_norm")
	ctx := context.Background()

	s, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// 1. Direct unnormalized "Alice.Smith" violates organisations_name_check in postgres
	_, err = s.Queries().CreateOrganisation(ctx, "Alice.Smith")
	if err == nil {
		t.Fatalf("expected unnormalized 'Alice.Smith' to violate organisations_name_check, got nil")
	}

	// 2. Normalized "alice_smith" passes constraint
	normName := "alice_smith_" + uuid.NewString()[:8]
	org, err := s.Queries().CreateOrganisation(ctx, normName)
	if err != nil {
		t.Fatalf("expected normalized '%s' to succeed, got %v", normName, err)
	}
	if org.Name != normName {
		t.Errorf("expected org name '%s', got %q", normName, org.Name)
	}
}
