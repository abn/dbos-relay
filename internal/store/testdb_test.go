package store

import (
	"context"
	"testing"

	"github.com/abn/relay/internal/testdb"
)

func testStore(t *testing.T) *Store {
	t.Helper()

	url := testdb.OpenStore(t, "store")

	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("failed to open test store: %v", err)
	}

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate test store: %v", err)
	}

	t.Cleanup(func() {
		if err := s.Truncate(context.Background()); err != nil {
			t.Errorf("failed to truncate test store: %v", err)
		}
		s.Close()
	})

	return s
}

func TestLiveDatabase_Reachable(t *testing.T) {
	url := testdb.OpenStore(t, "store")

	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("failed to connect to live database at %s: %v", url, err)
	}
	defer s.Close()

	if err := s.Ping(ctx); err != nil {
		t.Fatalf("failed to ping live database at %s: %v", url, err)
	}
}
