package ha_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/ha"
	"github.com/abn/relay/internal/store/gen"
)

type mockInstanceStore struct {
	mu             sync.Mutex
	upsertCalled   bool
	heartbeatCalls int
	adoptCalls     int
	deleteCalled   bool
}

func (m *mockInstanceStore) UpsertInstance(ctx context.Context, arg gen.UpsertInstanceParams) (gen.Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsertCalled = true
	return gen.Instance{
		ID:               arg.ID,
		AdvertiseAddress: arg.AdvertiseAddress,
		Port:             arg.Port,
	}, nil
}

func (m *mockInstanceStore) HeartbeatInstance(ctx context.Context, id pgtype.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeatCalls++
	return nil
}

func (m *mockInstanceStore) DeleteInstance(ctx context.Context, id pgtype.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalled = true
	return nil
}

func (m *mockInstanceStore) AdoptExpiredExecutors(ctx context.Context, arg gen.AdoptExpiredExecutorsParams) ([]gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adoptCalls++
	return nil, nil
}

func (m *mockInstanceStore) DeleteStaleInstances(ctx context.Context, cutoff pgtype.Timestamptz) (int64, error) {
	return 0, nil
}

func TestInstanceManager_Lifecycle(t *testing.T) {
	mock := &mockInstanceStore{}
	instID := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true}

	opts := ha.ManagerOptions{
		InstanceID:        instID,
		AdvertiseAddress:  "127.0.0.1",
		Port:              8090,
		HeartbeatInterval: 20 * time.Millisecond,
		AdoptionInterval:  20 * time.Millisecond,
		LeaseDuration:     1 * time.Minute,
	}

	mgr := ha.NewManager(mock, opts)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if mgr.ID() != instID {
		t.Errorf("expected ID %v, got %v", instID, mgr.ID())
	}
	if mgr.AdvertiseAddress() != "127.0.0.1" {
		t.Errorf("expected address 127.0.0.1, got %q", mgr.AdvertiseAddress())
	}
	if mgr.Port() != 8090 {
		t.Errorf("expected port 8090, got %d", mgr.Port())
	}

	// Wait for ticker iterations
	time.Sleep(70 * time.Millisecond)

	mock.mu.Lock()
	if !mock.upsertCalled {
		t.Errorf("expected UpsertInstance to have been called")
	}
	if mock.heartbeatCalls < 2 {
		t.Errorf("expected at least 2 heartbeat calls, got %d", mock.heartbeatCalls)
	}
	if mock.adoptCalls < 2 {
		t.Errorf("expected at least 2 adopt calls, got %d", mock.adoptCalls)
	}
	mock.mu.Unlock()

	mgr.Stop()

	mock.mu.Lock()
	if !mock.deleteCalled {
		t.Errorf("expected DeleteInstance to have been called on Stop")
	}
	mock.mu.Unlock()
}
