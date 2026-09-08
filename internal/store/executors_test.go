package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

func TestExecutors(t *testing.T) {
	s := testStore(t)
	if s == nil {
		t.Skip("skipping test; no database")
	}

	ctx := context.Background()

	appID := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true}
	executorID := "exec-1"
	ownerID := pgtype.UUID{Bytes: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, Valid: true}

	t.Run("UpsertExecutor", func(t *testing.T) {
		leaseExpiresAt := pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true}

		// Insert new
		exec, err := s.Queries().UpsertExecutor(ctx, gen.UpsertExecutorParams{
			ApplicationID:      appID,
			ExecutorID:         executorID,
			ApplicationVersion: "v1",
			Hostname:           "host-1",
			Metadata:           []byte(`{"key":"value"}`),
			OwnerInstanceID:    ownerID,
			LeaseExpiresAt:     leaseExpiresAt,
		})
		if err != nil {
			t.Fatalf("UpsertExecutor failed: %v", err)
		}
		if exec.Status != "connected" {
			t.Errorf("expected status 'connected', got %q", exec.Status)
		}
		if exec.ApplicationVersion != "v1" {
			t.Errorf("expected version 'v1', got %q", exec.ApplicationVersion)
		}
		if exec.Hostname != "host-1" {
			t.Errorf("expected hostname 'host-1', got %q", exec.Hostname)
		}

		time.Sleep(10 * time.Millisecond) // Ensure time moves forward for last_seen_at check

		// Upsert existing
		exec2, err := s.Queries().UpsertExecutor(ctx, gen.UpsertExecutorParams{
			ApplicationID:      appID,
			ExecutorID:         executorID,
			ApplicationVersion: "v2",
			Hostname:           "host-2",
			Metadata:           []byte(`{"key":"value2"}`),
			OwnerInstanceID:    ownerID,
			LeaseExpiresAt:     leaseExpiresAt,
		})
		if err != nil {
			t.Fatalf("UpsertExecutor (update) failed: %v", err)
		}
		if exec2.Status != "connected" {
			t.Errorf("expected status 'connected', got %q", exec2.Status)
		}
		if exec2.ApplicationVersion != "v2" {
			t.Errorf("expected version 'v2', got %q", exec2.ApplicationVersion)
		}
		if exec2.Hostname != "host-2" {
			t.Errorf("expected hostname 'host-2', got %q", exec2.Hostname)
		}
		if !exec2.LastSeenAt.Time.After(exec.LastSeenAt.Time) {
			t.Errorf("expected last_seen_at to increase, but it did not")
		}
	})

	t.Run("TouchExecutorLastSeen", func(t *testing.T) {
		execBefore, err := s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
			ApplicationID: appID,
			ExecutorID:    executorID,
		})
		if err != nil {
			t.Fatalf("GetExecutorByID failed: %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		newLease := pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true}
		err = s.Queries().TouchExecutorLastSeen(ctx, gen.TouchExecutorLastSeenParams{
			ApplicationID:  appID,
			ExecutorID:     executorID,
			LeaseExpiresAt: newLease,
		})
		if err != nil {
			t.Fatalf("TouchExecutorLastSeen failed: %v", err)
		}

		execAfter, err := s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
			ApplicationID: appID,
			ExecutorID:    executorID,
		})
		if err != nil {
			t.Fatalf("GetExecutorByID failed: %v", err)
		}
		if !execAfter.LastSeenAt.Time.After(execBefore.LastSeenAt.Time) {
			t.Errorf("expected last_seen_at to update on touch")
		}
		if execAfter.LeaseExpiresAt.Time.Unix() != newLease.Time.Unix() {
			t.Errorf("expected lease expiration %v, got %v", newLease.Time.Unix(), execAfter.LeaseExpiresAt.Time.Unix())
		}
	})

	t.Run("DisconnectExecutor", func(t *testing.T) {
		exec, err := s.Queries().DisconnectExecutor(ctx, gen.DisconnectExecutorParams{
			ApplicationID: appID,
			ExecutorID:    executorID,
		})
		if err != nil {
			t.Fatalf("DisconnectExecutor failed: %v", err)
		}
		if exec.Status != "disconnected" {
			t.Errorf("expected status 'disconnected', got %q", exec.Status)
		}
		if !exec.DisconnectedAt.Valid {
			t.Errorf("expected disconnected_at to be valid")
		}
		if exec.OwnerInstanceID.Valid {
			t.Errorf("expected owner_instance_id to be cleared")
		}
		if exec.LeaseExpiresAt.Valid {
			t.Errorf("expected lease_expires_at to be cleared")
		}
	})

	t.Run("ListConnectedExecutorsByApplication", func(t *testing.T) {
		execs, err := s.Queries().ListConnectedExecutorsByApplication(ctx, appID)
		if err != nil {
			t.Fatalf("ListConnectedExecutorsByApplication failed: %v", err)
		}
		if len(execs) != 0 {
			t.Errorf("expected 0 connected executors, got %d", len(execs))
		}

		// Reconnect it
		leaseExpiresAt := pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true}
		_, err = s.Queries().UpsertExecutor(ctx, gen.UpsertExecutorParams{
			ApplicationID:      appID,
			ExecutorID:         executorID,
			ApplicationVersion: "v2",
			Hostname:           "host-2",
			Metadata:           []byte(`{"key":"value2"}`),
			OwnerInstanceID:    ownerID,
			LeaseExpiresAt:     leaseExpiresAt,
		})
		if err != nil {
			t.Fatalf("UpsertExecutor reconnect failed: %v", err)
		}

		execs, err = s.Queries().ListConnectedExecutorsByApplication(ctx, appID)
		if err != nil {
			t.Fatalf("ListConnectedExecutorsByApplication failed: %v", err)
		}
		if len(execs) != 1 {
			t.Fatalf("expected 1 connected executor, got %d", len(execs))
		}
		if execs[0].ExecutorID != executorID {
			t.Errorf("expected executor_id %q, got %q", executorID, execs[0].ExecutorID)
		}
		if execs[0].Status != "connected" {
			t.Errorf("expected status 'connected', got %q", execs[0].Status)
		}
	})

	t.Run("SetExecutorDeadAndListDead", func(t *testing.T) {
		deadExec, err := s.Queries().SetExecutorDead(ctx, gen.SetExecutorDeadParams{
			ApplicationID: appID,
			ExecutorID:    executorID,
		})
		if err != nil {
			t.Fatalf("SetExecutorDead failed: %v", err)
		}
		if deadExec.Status != "dead" {
			t.Errorf("expected status 'dead', got %q", deadExec.Status)
		}
		if deadExec.OwnerInstanceID.Valid {
			t.Errorf("expected owner_instance_id to be cleared on dead")
		}

		deadList, err := s.Queries().ListDeadExecutorsByApplication(ctx, appID)
		if err != nil {
			t.Fatalf("ListDeadExecutorsByApplication failed: %v", err)
		}
		if len(deadList) != 1 || deadList[0].ExecutorID != executorID {
			t.Fatalf("expected 1 dead executor with ID %q, got %+v", executorID, deadList)
		}
	})

	t.Run("AdoptExpiredExecutors", func(t *testing.T) {
		// Re-upsert as connected with expired lease
		expiredLease := pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Minute), Valid: true}
		_, err := s.Queries().UpsertExecutor(ctx, gen.UpsertExecutorParams{
			ApplicationID:      appID,
			ExecutorID:         executorID,
			ApplicationVersion: "v1",
			OwnerInstanceID:    ownerID,
			LeaseExpiresAt:     expiredLease,
		})
		if err != nil {
			t.Fatalf("UpsertExecutor with expired lease failed: %v", err)
		}

		newInstanceID := pgtype.UUID{Bytes: [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}, Valid: true}
		newLease := pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true}

		adopted, err := s.Queries().AdoptExpiredExecutors(ctx, gen.AdoptExpiredExecutorsParams{
			OwnerInstanceID: newInstanceID,
			LeaseExpiresAt:  newLease,
		})
		if err != nil {
			t.Fatalf("AdoptExpiredExecutors failed: %v", err)
		}
		if len(adopted) == 0 {
			t.Fatalf("expected at least 1 adopted executor")
		}
		found := false
		for _, e := range adopted {
			if e.ExecutorID == executorID {
				found = true
				if e.OwnerInstanceID != newInstanceID {
					t.Errorf("expected owner %v, got %v", newInstanceID, e.OwnerInstanceID)
				}
				break
			}
		}
		if !found {
			t.Errorf("did not find %q in adopted list", executorID)
		}
	})

	t.Run("DeleteExecutor", func(t *testing.T) {
		err := s.Queries().DeleteExecutor(ctx, gen.DeleteExecutorParams{
			ApplicationID: appID,
			ExecutorID:    executorID,
		})
		if err != nil {
			t.Fatalf("DeleteExecutor failed: %v", err)
		}

		_, err = s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
			ApplicationID: appID,
			ExecutorID:    executorID,
		})
		if err == nil {
			t.Fatalf("expected error querying deleted executor, got nil")
		}
	})
}
