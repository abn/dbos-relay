package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

func TestInstances(t *testing.T) {
	s := testStore(t)
	if s == nil {
		t.Skip("skipping test; no database")
	}

	ctx := context.Background()
	instanceID := pgtype.UUID{Bytes: [16]byte{9, 9, 9, 9, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, Valid: true}

	t.Run("UpsertInstance", func(t *testing.T) {
		inst, err := s.Queries().UpsertInstance(ctx, gen.UpsertInstanceParams{
			ID:               instanceID,
			AdvertiseAddress: "10.0.0.1",
			Port:             8090,
		})
		if err != nil {
			t.Fatalf("UpsertInstance failed: %v", err)
		}
		if inst.AdvertiseAddress != "10.0.0.1" || inst.Port != 8090 {
			t.Errorf("unexpected instance data: %+v", inst)
		}

		// Update existing instance
		inst2, err := s.Queries().UpsertInstance(ctx, gen.UpsertInstanceParams{
			ID:               instanceID,
			AdvertiseAddress: "10.0.0.2",
			Port:             8091,
		})
		if err != nil {
			t.Fatalf("UpsertInstance update failed: %v", err)
		}
		if inst2.AdvertiseAddress != "10.0.0.2" || inst2.Port != 8091 {
			t.Errorf("unexpected updated instance data: %+v", inst2)
		}
	})

	t.Run("GetInstance", func(t *testing.T) {
		inst, err := s.Queries().GetInstance(ctx, instanceID)
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}
		if inst.ID != instanceID {
			t.Errorf("expected ID %v, got %v", instanceID, inst.ID)
		}
	})

	t.Run("ListHealthyInstances", func(t *testing.T) {
		cutoff := pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Minute), Valid: true}
		list, err := s.Queries().ListHealthyInstances(ctx, cutoff)
		if err != nil {
			t.Fatalf("ListHealthyInstances failed: %v", err)
		}
		if len(list) == 0 {
			t.Errorf("expected at least 1 healthy instance, got 0")
		}
	})

	t.Run("HeartbeatInstance", func(t *testing.T) {
		err := s.Queries().HeartbeatInstance(ctx, instanceID)
		if err != nil {
			t.Fatalf("HeartbeatInstance failed: %v", err)
		}
	})

	t.Run("DeleteInstance", func(t *testing.T) {
		err := s.Queries().DeleteInstance(ctx, instanceID)
		if err != nil {
			t.Fatalf("DeleteInstance failed: %v", err)
		}
		_, err = s.Queries().GetInstance(ctx, instanceID)
		if err == nil {
			t.Fatalf("expected error getting deleted instance, got nil")
		}
	})
}
