package liveness_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/liveness"
	"github.com/abn/relay/internal/store/gen"
)

type mockStoreQueries struct {
	mu           sync.Mutex
	apps         map[pgtype.UUID]gen.Application
	executors    map[string]gen.Executor
	deadCalls    []string
	deletedCalls []string
}

func newMockStoreQueries() *mockStoreQueries {
	return &mockStoreQueries{
		apps:      make(map[pgtype.UUID]gen.Application),
		executors: make(map[string]gen.Executor),
	}
}

func (m *mockStoreQueries) GetApplicationByID(ctx context.Context, id pgtype.UUID) (gen.Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app, ok := m.apps[id]
	if !ok {
		return gen.Application{}, errors.New("app not found")
	}
	return app, nil
}

func (m *mockStoreQueries) DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.executors[arg.ExecutorID]
	e.Status = "disconnected"
	m.executors[arg.ExecutorID] = e
	return e, nil
}

func (m *mockStoreQueries) SetExecutorDead(ctx context.Context, arg gen.SetExecutorDeadParams) (gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deadCalls = append(m.deadCalls, arg.ExecutorID)
	e := m.executors[arg.ExecutorID]
	e.Status = "dead"
	m.executors[arg.ExecutorID] = e
	return e, nil
}

func (m *mockStoreQueries) DeleteExecutor(ctx context.Context, arg gen.DeleteExecutorParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedCalls = append(m.deletedCalls, arg.ExecutorID)
	delete(m.executors, arg.ExecutorID)
	return nil
}

type mockRecoveryRunner struct {
	mu       sync.Mutex
	calls    []string
	versions []string
	ch       chan string
}

func newMockRecoveryRunner() *mockRecoveryRunner {
	return &mockRecoveryRunner{
		ch: make(chan string, 10),
	}
}

func (m *mockRecoveryRunner) RecoverDeadExecutor(ctx context.Context, appID pgtype.UUID, deadExecutorID, deadVersion string) error {
	m.mu.Lock()
	m.calls = append(m.calls, deadExecutorID)
	m.versions = append(m.versions, deadVersion)
	m.mu.Unlock()

	select {
	case m.ch <- deadExecutorID:
	default:
	}
	return nil
}

func TestManager_GracePeriodAndRecovery(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()
	mgr := liveness.NewManager(clock, store, recovery, nil)
	defer mgr.Stop()

	appID := pgtype.UUID{Bytes: [16]byte{10}, Valid: true}
	execID := "exec-test-1"
	ctx := context.Background()

	// 1. Connect
	if err := mgr.OnConnect(ctx, appID, execID, "v1.0.0"); err != nil {
		t.Fatalf("OnConnect failed: %v", err)
	}

	// 2. Disconnect (default grace 60s)
	mgr.OnDisconnect(ctx, appID, execID)

	// Advance virtual time by 30s: executor is still disconnected, NOT dead
	clock.Advance(30 * time.Second)

	select {
	case <-recovery.ch:
		t.Fatal("recovery triggered prematurely before 60s timeout")
	default:
	}

	// Advance another 31s (total 61s): grace period expires
	clock.Advance(31 * time.Second)

	select {
	case deadID := <-recovery.ch:
		if deadID != execID {
			t.Fatalf("expected recovery for %q, got %q", execID, deadID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for recovery dispatch notification")
	}

	store.mu.Lock()
	if len(store.deadCalls) != 1 || store.deadCalls[0] != execID {
		t.Fatalf("expected SetExecutorDead called for %q, got %v", execID, store.deadCalls)
	}
	store.mu.Unlock()

	// 3. Attempting to reconnect dead executor is rejected
	err := mgr.OnConnect(ctx, appID, execID, "v1.0.0")
	if !errors.Is(err, liveness.ErrReconnectingDeadExecutor) {
		t.Fatalf("expected ErrReconnectingDeadExecutor, got %v", err)
	}
}

func TestManager_ReconnectWithinGraceCancelsTimer(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()
	mgr := liveness.NewManager(clock, store, recovery, nil)
	defer mgr.Stop()

	appID := pgtype.UUID{Bytes: [16]byte{11}, Valid: true}
	execID := "exec-test-2"
	ctx := context.Background()

	// Connect
	if err := mgr.OnConnect(ctx, appID, execID, "v1.0.0"); err != nil {
		t.Fatalf("OnConnect failed: %v", err)
	}

	// Disconnect
	mgr.OnDisconnect(ctx, appID, execID)

	// Advance 20s
	clock.Advance(20 * time.Second)

	// Reconnect before 60s timeout
	if err := mgr.OnConnect(ctx, appID, execID, "v1.0.0"); err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}

	// Advance another 60s
	clock.Advance(60 * time.Second)

	select {
	case deadID := <-recovery.ch:
		t.Fatalf("unexpected recovery triggered for %q", deadID)
	default:
	}

	store.mu.Lock()
	if len(store.deadCalls) != 0 {
		t.Fatalf("expected 0 dead calls, got %v", store.deadCalls)
	}
	store.mu.Unlock()
}

func TestManager_CustomApplicationTimeout(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()

	appID := pgtype.UUID{Bytes: [16]byte{12}, Valid: true}
	settingsJSON, _ := json.Marshal(map[string]int64{"executorTimeoutSecs": 15})
	store.apps[appID] = gen.Application{
		ID:       appID,
		Settings: settingsJSON,
	}

	mgr := liveness.NewManager(clock, store, recovery, nil)
	defer mgr.Stop()

	execID := "exec-test-3"
	ctx := context.Background()

	if err := mgr.OnConnect(ctx, appID, execID, "v1.0.0"); err != nil {
		t.Fatalf("OnConnect failed: %v", err)
	}

	mgr.OnDisconnect(ctx, appID, execID)

	// 10s: should NOT expire
	clock.Advance(10 * time.Second)
	select {
	case <-recovery.ch:
		t.Fatal("premature expiration at 10s")
	default:
	}

	// 6s (total 16s > 15s): should expire
	clock.Advance(6 * time.Second)
	select {
	case deadID := <-recovery.ch:
		if deadID != execID {
			t.Fatalf("expected %q, got %q", execID, deadID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for recovery")
	}
}

type delayedStoreQueries struct {
	*mockStoreQueries
	getAppStarted chan struct{}
	getAppRelease chan struct{}
}

func (d *delayedStoreQueries) GetApplicationByID(ctx context.Context, id pgtype.UUID) (gen.Application, error) {
	close(d.getAppStarted)
	<-d.getAppRelease
	return d.mockStoreQueries.GetApplicationByID(ctx, id)
}

func TestManager_OnDisconnect_DoesNotHoldLockDuringDBQuery(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	delayedStore := &delayedStoreQueries{
		mockStoreQueries: newMockStoreQueries(),
		getAppStarted:    make(chan struct{}),
		getAppRelease:    make(chan struct{}),
	}
	appID := pgtype.UUID{Bytes: [16]byte{20}, Valid: true}
	delayedStore.apps[appID] = gen.Application{ID: appID}

	mgr := liveness.NewManager(clock, delayedStore, nil, nil)
	defer mgr.Stop()

	ctx := context.Background()
	_ = mgr.OnConnect(ctx, appID, "exec-dc-1", "v1.0.0")

	// Call OnDisconnect asynchronously; it will enter resolveTimeout and block on getAppRelease
	go func() {
		mgr.OnDisconnect(ctx, appID, "exec-dc-1")
	}()

	// Wait until GetApplicationByID is in-flight
	<-delayedStore.getAppStarted

	// Verify another operation (OnConnect) is not blocked on manager mutex while DB query is running
	connectDone := make(chan error, 1)
	go func() {
		connectDone <- mgr.OnConnect(ctx, appID, "exec-concurrent", "v1.0.0")
	}()

	select {
	case err := <-connectDone:
		if err != nil {
			t.Fatalf("concurrent OnConnect failed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("OnConnect blocked while OnDisconnect was executing database query")
	}

	// Release the DB query and finish
	close(delayedStore.getAppRelease)
}

func TestManager_DeadExecutorEvictionAndReconnection(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()
	mgr := liveness.NewManager(clock, store, recovery, nil)
	defer mgr.Stop()

	appID := pgtype.UUID{Bytes: [16]byte{21}, Valid: true}
	execID := "exec-dead-evict"
	ctx := context.Background()

	if err := mgr.OnConnect(ctx, appID, execID, "v1.0.0"); err != nil {
		t.Fatalf("initial connect failed: %v", err)
	}

	// Disconnect and let grace period expire
	mgr.OnDisconnect(ctx, appID, execID)
	clock.Advance(65 * time.Second)

	select {
	case <-recovery.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for recovery")
	}

	// Verify executor is declared dead
	err := mgr.OnConnect(ctx, appID, execID, "v1.0.0")
	if !errors.Is(err, liveness.ErrReconnectingDeadExecutor) {
		t.Fatalf("expected ErrReconnectingDeadExecutor while dead, got: %v", err)
	}

	// Delete/evict executor from manager (as triggered upon recovery ack)
	err = mgr.DeleteExecutor(ctx, gen.DeleteExecutorParams{
		ApplicationID: appID,
		ExecutorID:    execID,
	})
	if err != nil {
		t.Fatalf("DeleteExecutor failed: %v", err)
	}

	// Now executor can reconnect cleanly
	if err := mgr.OnConnect(ctx, appID, execID, "v2.0.0"); err != nil {
		t.Fatalf("reconnect after eviction failed: %v", err)
	}
}
