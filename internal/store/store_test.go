package store

import (
	"context"
	"strings"
	"testing"
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
		"ab",           // too short
		"Not-Lower",    // uppercase and hyphen
		"has space",    // space
		"",             // empty
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
