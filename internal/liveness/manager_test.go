package liveness_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
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

func (m *mockStoreQueries) GetExecutorByID(ctx context.Context, arg gen.GetExecutorByIDParams) (gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.executors[arg.ExecutorID]
	if !ok {
		return gen.Executor{}, errors.New("not found")
	}
	return e, nil
}

func (m *mockStoreQueries) ListDeadExecutorsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []gen.Executor
	for _, e := range m.executors {
		if e.Status == "dead" {
			res = append(res, e)
		}
	}
	return res, nil
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

type spyClock struct {
	liveness.Clock
	mu           sync.Mutex
	lastDuration time.Duration
	timerCalls   int
}

func newSpyClock(base liveness.Clock) *spyClock {
	return &spyClock{Clock: base}
}

func (s *spyClock) NewTimer(d time.Duration) liveness.Timer {
	s.mu.Lock()
	s.lastDuration = d
	s.timerCalls++
	s.mu.Unlock()
	return s.Clock.NewTimer(d)
}

func (s *spyClock) LastDuration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastDuration
}

func TestManager_FlapGoroutineLeak(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()
	mgr := liveness.NewManager(clock, store, recovery, nil)
	defer mgr.Stop()

	appID := pgtype.UUID{Bytes: [16]byte{55}, Valid: true}
	execID := "flap-exec"
	ctx := context.Background()

	if err := mgr.OnConnect(ctx, appID, execID, "v1"); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	baselineGoroutines := runtime.NumGoroutine()

	for i := 0; i < 500; i++ {
		mgr.OnDisconnect(ctx, appID, execID)
		if err := mgr.OnConnect(ctx, appID, execID, "v1"); err != nil {
			t.Fatalf("reconnect failed at cycle %d: %v", i, err)
		}
	}

	time.Sleep(50 * time.Millisecond)
	currentGoroutines := runtime.NumGoroutine()
	if currentGoroutines > baselineGoroutines+5 {
		t.Fatalf("leaked goroutines: before=%d, after=%d", baselineGoroutines, currentGoroutines)
	}
}

func TestManager_FlapGoroutineLeak_RealClock(t *testing.T) {
	clock := liveness.NewRealClock()
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()
	mgr := liveness.NewManager(clock, store, recovery, nil)
	defer mgr.Stop()

	appID := pgtype.UUID{Bytes: [16]byte{56}, Valid: true}
	execID := "flap-exec-real"
	ctx := context.Background()

	if err := mgr.OnConnect(ctx, appID, execID, "v1"); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	baselineGoroutines := runtime.NumGoroutine()

	for i := 0; i < 500; i++ {
		mgr.OnDisconnect(ctx, appID, execID)
		if err := mgr.OnConnect(ctx, appID, execID, "v1"); err != nil {
			t.Fatalf("reconnect failed at cycle %d: %v", i, err)
		}
	}

	time.Sleep(50 * time.Millisecond)
	currentGoroutines := runtime.NumGoroutine()
	if currentGoroutines > baselineGoroutines+5 {
		t.Fatalf("leaked goroutines under real clock: before=%d, after=%d", baselineGoroutines, currentGoroutines)
	}
}

func TestManager_ApplicationTimeoutResolutionAndFloor(t *testing.T) {
	baseClock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	spy := newSpyClock(baseClock)
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()
	mgr := liveness.NewManager(spy, store, recovery, nil)
	defer mgr.Stop()

	ctx := context.Background()

	// 1. Default timeout when app not found or empty settings
	appDefaultID := pgtype.UUID{Bytes: [16]byte{61}, Valid: true}
	_ = mgr.OnConnect(ctx, appDefaultID, "exec-default", "v1")
	mgr.OnDisconnect(ctx, appDefaultID, "exec-default")
	if dur := spy.LastDuration(); dur != 60*time.Second {
		t.Fatalf("expected default 60s timeout, got %v", dur)
	}

	// 2. Custom 15s configured timeout
	app15ID := pgtype.UUID{Bytes: [16]byte{62}, Valid: true}
	store.apps[app15ID] = gen.Application{
		ID:       app15ID,
		Settings: []byte(`{"executorTimeoutSecs": 15}`),
	}
	_ = mgr.OnConnect(ctx, app15ID, "exec-15", "v1")
	mgr.OnDisconnect(ctx, app15ID, "exec-15")
	if dur := spy.LastDuration(); dur != 15*time.Second {
		t.Fatalf("expected custom 15s timeout, got %v", dur)
	}

	// 3. Timeout of 0 falls back to 60s floor
	appZeroID := pgtype.UUID{Bytes: [16]byte{63}, Valid: true}
	store.apps[appZeroID] = gen.Application{
		ID:       appZeroID,
		Settings: []byte(`{"executorTimeoutSecs": 0}`),
	}
	_ = mgr.OnConnect(ctx, appZeroID, "exec-zero", "v1")
	mgr.OnDisconnect(ctx, appZeroID, "exec-zero")
	if dur := spy.LastDuration(); dur != 60*time.Second {
		t.Fatalf("expected floor 60s timeout for 0s config, got %v", dur)
	}

	// 4. Negative timeout falls back to 60s floor
	appNegID := pgtype.UUID{Bytes: [16]byte{64}, Valid: true}
	store.apps[appNegID] = gen.Application{
		ID:       appNegID,
		Settings: []byte(`{"executorTimeoutSecs": -10}`),
	}
	_ = mgr.OnConnect(ctx, appNegID, "exec-neg", "v1")
	mgr.OnDisconnect(ctx, appNegID, "exec-neg")
	if dur := spy.LastDuration(); dur != 60*time.Second {
		t.Fatalf("expected floor 60s timeout for negative config, got %v", dur)
	}
}

func TestManager_SlowDBLookupDoesNotBlockOnConnect(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := &slowMockStore{delay: 2 * time.Second}
	recovery := newMockRecoveryRunner()
	mgr := liveness.NewManager(clock, store, recovery, nil)
	defer mgr.Stop()

	appID := pgtype.UUID{Bytes: [16]byte{71}, Valid: true}
	ctx := context.Background()

	start := time.Now()
	err := mgr.OnConnect(ctx, appID, "exec-fast", "v1")
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("OnConnect failed: %v", err)
	}
	if duration > 100*time.Millisecond {
		t.Fatalf("OnConnect took %v, expected < 100ms", duration)
	}
}

type slowMockStore struct {
	mockStoreQueries
	delay time.Duration
}

func (s *slowMockStore) GetApplicationByID(ctx context.Context, id pgtype.UUID) (gen.Application, error) {
	select {
	case <-ctx.Done():
		return gen.Application{}, ctx.Err()
	case <-time.After(s.delay):
		return gen.Application{}, errors.New("timeout")
	}
}

func TestManager_HighCycleConnectDieRecover(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()
	mgr := liveness.NewManager(clock, store, recovery, nil)
	defer mgr.Stop()

	appID := pgtype.UUID{Bytes: [16]byte{88}, Valid: true}
	ctx := context.Background()

	for i := 0; i < 1000; i++ {
		execID := fmt.Sprintf("exec-cycle-%d", i)
		if err := mgr.OnConnect(ctx, appID, execID, "v1"); err != nil {
			t.Fatalf("connect failed at cycle %d: %v", i, err)
		}
		mgr.OnDisconnect(ctx, appID, execID)
		clock.Advance(65 * time.Second)
		select {
		case <-recovery.ch:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for recovery at cycle %d", i)
		}
		if err := mgr.DeleteExecutor(ctx, gen.DeleteExecutorParams{
			ApplicationID: appID,
			ExecutorID:    execID,
		}); err != nil {
			t.Fatalf("delete failed at cycle %d: %v", i, err)
		}
	}

	if count := mgr.TrackedCount(); count != 0 {
		t.Fatalf("expected 0 tracked executors after 1000 cycles, got %d", count)
	}

	// Subsequent reconnect for one of them succeeds cleanly
	if err := mgr.OnConnect(ctx, appID, "exec-cycle-0", "v1"); err != nil {
		t.Fatalf("reconnect failed after cycles: %v", err)
	}
}

func TestManager_SweepDeadExecutorsOnPeerConnect(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()
	mgr := liveness.NewManager(clock, store, recovery, nil)
	defer mgr.Stop()

	appID := pgtype.UUID{Bytes: [16]byte{91}, Valid: true}
	deadExecID := "exec-dead-alone"
	ctx := context.Background()

	// Seed a dead executor in the store
	store.executors[deadExecID] = gen.Executor{
		ApplicationID:      appID,
		ExecutorID:         deadExecID,
		ApplicationVersion: "v1.0.0",
		Status:             "dead",
	}

	// Peer connects
	peerExecID := "exec-healthy-peer"
	if err := mgr.OnConnect(ctx, appID, peerExecID, "v1.0.0"); err != nil {
		t.Fatalf("peer connect failed: %v", err)
	}

	// Sweep should trigger recovery for the dead executor
	select {
	case recID := <-recovery.ch:
		if recID != deadExecID {
			t.Fatalf("expected recovery for %s, got %s", deadExecID, recID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for peer connect sweep recovery")
	}
}

func TestManager_ClusterReconnectRace(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newMockStoreQueries()
	recovery := newMockRecoveryRunner()
	mgrA := liveness.NewManager(clock, store, recovery, nil)
	defer mgrA.Stop()

	instA := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	instB := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	mgrA.SetInstanceID(instA)

	appID := pgtype.UUID{Bytes: [16]byte{95}, Valid: true}
	execID := "exec-cluster-move"
	ctx := context.Background()

	// Connect to instance A
	if err := mgrA.OnConnect(ctx, appID, execID, "v1"); err != nil {
		t.Fatalf("connect to A failed: %v", err)
	}

	// Disconnect from instance A
	mgrA.OnDisconnect(ctx, appID, execID)

	// In the shared store, executor reconnected to instance B with an active future lease
	store.executors[execID] = gen.Executor{
		ApplicationID:   appID,
		ExecutorID:      execID,
		Status:          "connected",
		OwnerInstanceID: instB,
		LeaseExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(1 * time.Minute), Valid: true},
	}

	// Grace period timer on instance A expires
	clock.Advance(65 * time.Second)

	// Ensure no recovery dispatch is sent because executor is connected to instance B
	select {
	case recID := <-recovery.ch:
		t.Fatalf("unexpected recovery dispatch for %s which reconnected to instance B", recID)
	case <-time.After(100 * time.Millisecond):
		// Expected: no recovery dispatched
	}

	// Ensure SetExecutorDead was not called on store
	store.mu.Lock()
	if len(store.deadCalls) > 0 {
		t.Fatalf("SetExecutorDead called unexpectedly: %v", store.deadCalls)
	}
	store.mu.Unlock()

	// Ensure executor was evicted from A's tracking map
	if mgrA.TrackedCount() != 0 {
		t.Fatalf("expected 0 tracked executors on A, got %d", mgrA.TrackedCount())
	}
}
