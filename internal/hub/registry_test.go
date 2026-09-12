package hub

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestRegistry_RoundRobinUniformity(t *testing.T) {
	reg := NewRegistry(nil, nil)
	appID := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true}

	// 4 executors: 400 selections must yield 100 +/- 15 each (exactly 100 with round-robin)
	for i := 0; i < 4; i++ {
		reg.Register(&ExecutorConn{
			appID:      appID,
			executorID: fmt.Sprintf("ex-%d", i),
		})
	}

	counts4 := make(map[string]int)
	for i := 0; i < 400; i++ {
		conn, err := reg.SelectExecutor(appID)
		if err != nil {
			t.Fatalf("unexpected select error: %v", err)
		}
		counts4[conn.executorID]++
	}

	for id, count := range counts4 {
		if count < 85 || count > 115 {
			t.Errorf("expected count for %s to be 100 +/- 15, got %d", id, count)
		}
	}

	// 2 executors: 400 selections must yield 200 +/- 15 each (exactly 200 with round-robin)
	appID2 := pgtype.UUID{Bytes: [16]byte{5, 6, 7, 8}, Valid: true}
	for i := 0; i < 2; i++ {
		reg.Register(&ExecutorConn{
			appID:      appID2,
			executorID: fmt.Sprintf("ex-2-%d", i),
		})
	}

	counts2 := make(map[string]int)
	for i := 0; i < 400; i++ {
		conn, err := reg.SelectExecutor(appID2)
		if err != nil {
			t.Fatalf("unexpected select error: %v", err)
		}
		counts2[conn.executorID]++
	}

	for id, count := range counts2 {
		if count < 185 || count > 215 {
			t.Errorf("expected count for %s to be 200 +/- 15, got %d", id, count)
		}
	}
}

func TestRegistry_SelectExecutorWithExclusion(t *testing.T) {
	reg := NewRegistry(nil, nil)
	appID := pgtype.UUID{Bytes: [16]byte{9, 9, 9, 9}, Valid: true}

	reg.Register(&ExecutorConn{
		appID:      appID,
		executorID: "ex-a",
	})
	reg.Register(&ExecutorConn{
		appID:      appID,
		executorID: "ex-b",
	})

	for i := 0; i < 10; i++ {
		conn, err := reg.SelectExecutorWithExclusion(appID, "ex-a")
		if err != nil {
			t.Fatalf("unexpected select error: %v", err)
		}
		if conn.executorID != "ex-b" {
			t.Errorf("expected ex-b, got %s", conn.executorID)
		}
	}

	// When all executors are excluded
	regSingle := NewRegistry(nil, nil)
	singleApp := pgtype.UUID{Bytes: [16]byte{8, 8, 8, 8}, Valid: true}
	regSingle.Register(&ExecutorConn{
		appID:      singleApp,
		executorID: "only-one",
	})
	_, err := regSingle.SelectExecutorWithExclusion(singleApp, "only-one")
	if err == nil {
		t.Error("expected error when excluding the only executor, got nil")
	}
}
