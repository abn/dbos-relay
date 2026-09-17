package store_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
)

type mockRetentionStore struct {
	apps []gen.Application
}

func (m *mockRetentionStore) ListAllApplications(ctx context.Context) ([]gen.Application, error) {
	return m.apps, nil
}

func (m *mockRetentionStore) GetApplicationByID(ctx context.Context, id pgtype.UUID) (gen.Application, error) {
	for _, a := range m.apps {
		if a.ID == id {
			return a, nil
		}
	}
	return gen.Application{}, nil
}

type mockRetentionDispatcher struct {
	mu         sync.Mutex
	dispatched []protocol.Message
	count      atomic.Int32
}

func (m *mockRetentionDispatcher) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	m.mu.Lock()
	m.dispatched = append(m.dispatched, msg)
	m.mu.Unlock()
	m.count.Add(1)
	return &protocol.RetentionResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeRetention,
			RequestID: msg.GetRequestID(),
		},
		Success: true,
	}, nil
}

func TestBuildRetentionRequest(t *testing.T) {
	t.Run("EmptySettings", func(t *testing.T) {
		req, ok := store.BuildRetentionRequest(nil)
		if ok || req != nil {
			t.Errorf("expected false, nil for nil settings")
		}

		req, ok = store.BuildRetentionRequest([]byte(`{}`))
		if ok || req != nil {
			t.Errorf("expected false, nil for empty json")
		}

		req, ok = store.BuildRetentionRequest([]byte(`{"executorTimeoutSecs": 60}`))
		if ok || req != nil {
			t.Errorf("expected false, nil when no retention settings configured")
		}
	})

	t.Run("ConfiguredSettings", func(t *testing.T) {
		settingsJSON := []byte(`{
			"gcRowsThreshold": 50000,
			"gcTimeThresholdMs": 86400000,
			"globalTimeoutMs": 3600000
		}`)

		beforeMs := time.Now().UnixMilli()
		req, ok := store.BuildRetentionRequest(settingsJSON)
		afterMs := time.Now().UnixMilli()

		if !ok || req == nil {
			t.Fatalf("expected true, non-nil request")
		}
		if req.Type != protocol.MessageTypeRetention {
			t.Errorf("expected MessageTypeRetention, got %s", req.Type)
		}
		if req.Body.GCRowsThreshold == nil || *req.Body.GCRowsThreshold != 50000 {
			t.Errorf("expected gcRowsThreshold=50000, got %v", req.Body.GCRowsThreshold)
		}
		if req.Body.GCBatchSize == nil || *req.Body.GCBatchSize != 1000 {
			t.Errorf("expected gcBatchSize=1000, got %v", req.Body.GCBatchSize)
		}
		if req.Body.GCCutoffEpochMs == nil {
			t.Fatalf("expected non-nil gc_cutoff_epoch_ms")
		}
		expectedCutoff := beforeMs - 86400000
		if int64(*req.Body.GCCutoffEpochMs) < expectedCutoff-1000 || int64(*req.Body.GCCutoffEpochMs) > afterMs-86400000+1000 {
			t.Errorf("unexpected gc_cutoff_epoch_ms: %d", *req.Body.GCCutoffEpochMs)
		}
		if req.Body.TimeoutCutoffEpochMs == nil {
			t.Fatalf("expected non-nil timeout_cutoff_epoch_ms")
		}
		expectedTimeout := beforeMs - 3600000
		if int64(*req.Body.TimeoutCutoffEpochMs) < expectedTimeout-1000 || int64(*req.Body.TimeoutCutoffEpochMs) > afterMs-3600000+1000 {
			t.Errorf("unexpected timeout_cutoff_epoch_ms: %d", *req.Body.TimeoutCutoffEpochMs)
		}
	})
}

func TestRetentionScheduler_DispatchConnectRetention(t *testing.T) {
	appWithRetentionID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	appWithoutRetentionID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	mockStore := &mockRetentionStore{
		apps: []gen.Application{
			{
				ID:       appWithRetentionID,
				Name:     "app-with-retention",
				Settings: []byte(`{"gcRowsThreshold": 10000}`),
			},
			{
				ID:       appWithoutRetentionID,
				Name:     "app-without-retention",
				Settings: []byte(`{"executorTimeoutSecs": 30}`),
			},
		},
	}
	dispatcher := &mockRetentionDispatcher{}
	scheduler := store.NewRetentionScheduler(mockStore, dispatcher, nil)

	// App with retention should trigger dispatch
	err := scheduler.DispatchConnectRetention(context.Background(), appWithRetentionID)
	if err != nil {
		t.Fatalf("DispatchConnectRetention failed: %v", err)
	}
	if dispatcher.count.Load() != 1 {
		t.Fatalf("expected 1 dispatch for app with retention, got %d", dispatcher.count.Load())
	}

	// App without retention should be a no-op
	err = scheduler.DispatchConnectRetention(context.Background(), appWithoutRetentionID)
	if err != nil {
		t.Fatalf("DispatchConnectRetention failed: %v", err)
	}
	if dispatcher.count.Load() != 1 {
		t.Fatalf("expected dispatch count to remain 1, got %d", dispatcher.count.Load())
	}
}

func TestRetentionScheduler_RunOnceAndStart(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	mockStore := &mockRetentionStore{
		apps: []gen.Application{
			{
				ID:       appID,
				Name:     "app-1",
				Settings: []byte(`{"globalTimeoutMs": 60000}`),
			},
		},
	}
	dispatcher := &mockRetentionDispatcher{}
	scheduler := store.NewRetentionScheduler(mockStore, dispatcher, nil)

	// Single run
	err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if dispatcher.count.Load() != 1 {
		t.Fatalf("expected 1 dispatch from RunOnce, got %d", dispatcher.count.Load())
	}

	// Background ticker
	stop := scheduler.Start(context.Background(), 20*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	stop()

	if dispatcher.count.Load() < 2 {
		t.Fatalf("expected background loop to tick and dispatch, got %d", dispatcher.count.Load())
	}
}
