package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/abn/relay/internal/store/gen"
)

func TestMigrateIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Initial migration is done in testStore.
	// Running it again should not fail.
	err := s.Migrate(ctx)
	if err != nil {
		t.Fatalf("expected Migrate to be idempotent, got error: %v", err)
	}
}

func TestOrganisationNameConstraint(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Should succeed
	_, err := s.Queries().CreateOrganisation(ctx, "acme_corp")
	if err != nil {
		t.Fatalf("expected to create valid organisation 'acme_corp', got error: %v", err)
	}

	// Should fail: invalid names
	invalidNames := []string{
		"ab",        // too short
		"Not-Lower", // uppercase and hyphen
		"has space", // space
		"",          // empty
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			_, err := s.Queries().CreateOrganisation(ctx, name)
			if err == nil {
				t.Errorf("expected failure for invalid name %q, but it succeeded", name)
			} else if !strings.Contains(err.Error(), "organisations_name_check") {
				// PostgreSQL error for check constraint violation usually contains the constraint name
				t.Logf("got error for %q: %v", name, err)
			}
		})
	}
}

func TestGetAPIKeyByLookupRevoked(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	org, err := s.Queries().CreateOrganisation(ctx, "acme_auth_test")
	if err != nil {
		t.Fatalf("expected to create valid organisation, got error: %v", err)
	}

	key, err := s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             "test-key",
		Lookup:           "dbos_test_lookup",
		KeyHash:          []byte("testhash123"),
		ApplicationNames: []string{"test-app"},
		Permissions:      []string{"admin"},
	})
	if err != nil {
		t.Fatalf("expected to create api key, got error: %v", err)
	}

	found, err := s.Queries().GetAPIKeyByLookup(ctx, key.Lookup)
	if err != nil {
		t.Fatalf("expected key to be found before revocation, got error: %v", err)
	}
	if found.ID != key.ID {
		t.Fatalf("expected key ID %v, got %v", key.ID, found.ID)
	}

	_, err = s.Queries().RevokeAPIKey(ctx, gen.RevokeAPIKeyParams{
		ID:             key.ID,
		OrganisationID: org.ID,
	})
	if err != nil {
		t.Fatalf("expected to revoke api key, got error: %v", err)
	}

	_, err = s.Queries().GetAPIKeyByLookup(ctx, key.Lookup)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows for revoked key, got error: %v", err)
	}
}

func TestRenameAPIKey(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	org, err := s.Queries().CreateOrganisation(ctx, "acme_rename_test")
	if err != nil {
		t.Fatalf("expected to create valid organisation, got error: %v", err)
	}

	key, err := s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             "before",
		Lookup:           "dbos_rename_lookup",
		KeyHash:          []byte("testhash123"),
		ApplicationNames: []string{"test-app"},
		Permissions:      []string{"admin"},
	})
	if err != nil {
		t.Fatalf("expected to create api key, got error: %v", err)
	}

	renamed, err := s.Queries().RenameAPIKey(ctx, gen.RenameAPIKeyParams{
		ID:             key.ID,
		OrganisationID: org.ID,
		Name:           "after",
	})
	if err != nil {
		t.Fatalf("expected to rename api key, got error: %v", err)
	}
	if renamed.Name != "after" {
		t.Fatalf("expected renamed key name %q, got %q", "after", renamed.Name)
	}
	if renamed.Lookup != key.Lookup || string(renamed.KeyHash) != string(key.KeyHash) {
		t.Fatalf("expected rename to preserve lookup and hash")
	}

	if _, err := s.Queries().GetAPIKeyByName(ctx, gen.GetAPIKeyByNameParams{OrganisationID: org.ID, Name: "after"}); err != nil {
		t.Fatalf("expected renamed key to be found by new name, got error: %v", err)
	}
	if _, err := s.Queries().GetAPIKeyByName(ctx, gen.GetAPIKeyByNameParams{OrganisationID: org.ID, Name: "before"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected old name to be gone, got error: %v", err)
	}

	second, err := s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             "second",
		Lookup:           "dbos_rename_lookup_2",
		KeyHash:          []byte("testhash456"),
		ApplicationNames: []string{"test-app"},
		Permissions:      []string{"admin"},
	})
	if err != nil {
		t.Fatalf("expected to create second api key, got error: %v", err)
	}
	if _, err := s.Queries().RenameAPIKey(ctx, gen.RenameAPIKeyParams{ID: second.ID, OrganisationID: org.ID, Name: "after"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected rename collision to update no row, got error: %v", err)
	}
}

func TestStore_InTx(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	t.Run("commit", func(t *testing.T) {
		err := s.InTx(ctx, func(q gen.Querier) error {
			_, createErr := q.CreateOrganisation(ctx, "tx_org_commit")
			return createErr
		})
		if err != nil {
			t.Fatalf("expected InTx to commit successfully, got error: %v", err)
		}

		org, err := s.Queries().GetOrganisationByName(ctx, "tx_org_commit")
		if err != nil {
			t.Fatalf("expected organisation to exist after commit: %v", err)
		}
		if org.Name != "tx_org_commit" {
			t.Fatalf("expected organisation name tx_org_commit, got %s", org.Name)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		expectedErr := errors.New("simulated transaction failure")
		err := s.InTx(ctx, func(q gen.Querier) error {
			_, createErr := q.CreateOrganisation(ctx, "tx_org_rollback")
			if createErr != nil {
				return createErr
			}
			return expectedErr
		})
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected InTx to return %v, got %v", expectedErr, err)
		}

		_, err = s.Queries().GetOrganisationByName(ctx, "tx_org_rollback")
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("expected organisation to be rolled back and not exist, got error: %v", err)
		}
	})
}
