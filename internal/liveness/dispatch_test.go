package liveness_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/liveness"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

type mockPeerFinder struct {
	peers []liveness.Peer
	err   error
}

func (m *mockPeerFinder) FindHealthyPeers(ctx context.Context, appID pgtype.UUID) ([]liveness.Peer, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.peers, nil
}

type mockTransport struct {
	responses map[string]*protocol.RecoveryResponse
	errors    map[string]error
	calls     []string
}

func (m *mockTransport) SendRecovery(ctx context.Context, appID pgtype.UUID, targetExecutorID string, req *protocol.RecoveryRequest) (*protocol.RecoveryResponse, error) {
	m.calls = append(m.calls, targetExecutorID)
	if err, ok := m.errors[targetExecutorID]; ok {
		return nil, err
	}
	if resp, ok := m.responses[targetExecutorID]; ok {
		return resp, nil
	}
	return &protocol.RecoveryResponse{Success: true}, nil
}

type mockDeleter struct {
	deleted []string
}

func (m *mockDeleter) DeleteExecutor(ctx context.Context, arg gen.DeleteExecutorParams) error {
	m.deleted = append(m.deleted, arg.ExecutorID)
	return nil
}

func TestSelectCandidates(t *testing.T) {
	app1 := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	app2 := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	t.Run("Refuses cross-app peer", func(t *testing.T) {
		peers := []liveness.Peer{
			{AppID: app2, ExecutorID: "exec-foreign", ApplicationVersion: "v1"},
		}
		_, err := liveness.SelectCandidates(app1, "exec-dead", "v1", peers, true)
		if !errors.Is(err, liveness.ErrCrossTenantRecovery) {
			t.Fatalf("expected ErrCrossTenantRecovery, got %v", err)
		}
	})

	t.Run("Excludes dead executor itself", func(t *testing.T) {
		peers := []liveness.Peer{
			{AppID: app1, ExecutorID: "exec-dead", ApplicationVersion: "v1"},
		}
		_, err := liveness.SelectCandidates(app1, "exec-dead", "v1", peers, true)
		if !errors.Is(err, liveness.ErrNoHealthyPeers) {
			t.Fatalf("expected ErrNoHealthyPeers, got %v", err)
		}
	})

	t.Run("Prefers matching version", func(t *testing.T) {
		peers := []liveness.Peer{
			{AppID: app1, ExecutorID: "exec-v2", ApplicationVersion: "v2"},
			{AppID: app1, ExecutorID: "exec-v1", ApplicationVersion: "v1"},
		}
		candidates, err := liveness.SelectCandidates(app1, "exec-dead", "v1", peers, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(candidates) != 1 || candidates[0].ExecutorID != "exec-v1" {
			t.Fatalf("expected only matching version 'exec-v1', got %+v", candidates)
		}
	})

	t.Run("Refuses cross-version when disallowed", func(t *testing.T) {
		peers := []liveness.Peer{
			{AppID: app1, ExecutorID: "exec-v2", ApplicationVersion: "v2"},
		}
		_, err := liveness.SelectCandidates(app1, "exec-dead", "v1", peers, false)
		if !errors.Is(err, liveness.ErrVersionMismatchDisallowed) {
			t.Fatalf("expected ErrVersionMismatchDisallowed, got %v", err)
		}
	})

	t.Run("Allows cross-version when configured", func(t *testing.T) {
		peers := []liveness.Peer{
			{AppID: app1, ExecutorID: "exec-v2", ApplicationVersion: "v2"},
		}
		candidates, err := liveness.SelectCandidates(app1, "exec-dead", "v1", peers, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(candidates) != 1 || candidates[0].ExecutorID != "exec-v2" {
			t.Fatalf("expected fallback candidate 'exec-v2', got %+v", candidates)
		}
	})
}

func TestRecoverDeadExecutor(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	ctx := context.Background()

	t.Run("Successful recovery deletes dead executor record", func(t *testing.T) {
		finder := &mockPeerFinder{
			peers: []liveness.Peer{
				{AppID: appID, ExecutorID: "peer-1", ApplicationVersion: "v1"},
			},
		}
		transport := &mockTransport{
			responses: map[string]*protocol.RecoveryResponse{
				"peer-1": {Success: true},
			},
		}
		deleter := &mockDeleter{}

		dispatcher := liveness.NewRecoveryDispatcher(finder, transport, deleter, liveness.DispatcherOptions{})
		err := dispatcher.RecoverDeadExecutor(ctx, appID, "dead-1", "v1")
		if err != nil {
			t.Fatalf("RecoverDeadExecutor failed: %v", err)
		}

		if len(transport.calls) != 1 || transport.calls[0] != "peer-1" {
			t.Errorf("expected recovery dispatched to peer-1, got %v", transport.calls)
		}
		if len(deleter.deleted) != 1 || deleter.deleted[0] != "dead-1" {
			t.Errorf("expected dead-1 to be deleted, got %v", deleter.deleted)
		}
	})

	t.Run("Retries next peer when first peer fails", func(t *testing.T) {
		finder := &mockPeerFinder{
			peers: []liveness.Peer{
				{AppID: appID, ExecutorID: "peer-1", ApplicationVersion: "v1"},
				{AppID: appID, ExecutorID: "peer-2", ApplicationVersion: "v1"},
			},
		}
		transport := &mockTransport{
			errors: map[string]error{
				"peer-1": errors.New("connection reset"),
			},
			responses: map[string]*protocol.RecoveryResponse{
				"peer-2": {Success: true},
			},
		}
		deleter := &mockDeleter{}

		dispatcher := liveness.NewRecoveryDispatcher(finder, transport, deleter, liveness.DispatcherOptions{})
		err := dispatcher.RecoverDeadExecutor(ctx, appID, "dead-1", "v1")
		if err != nil {
			t.Fatalf("RecoverDeadExecutor failed: %v", err)
		}

		if len(transport.calls) != 2 || transport.calls[0] != "peer-1" || transport.calls[1] != "peer-2" {
			t.Errorf("expected recovery attempts to peer-1 then peer-2, got %v", transport.calls)
		}
		if len(deleter.deleted) != 1 || deleter.deleted[0] != "dead-1" {
			t.Errorf("expected dead-1 to be deleted, got %v", deleter.deleted)
		}
	})

	t.Run("Fails when all peers fail", func(t *testing.T) {
		finder := &mockPeerFinder{
			peers: []liveness.Peer{
				{AppID: appID, ExecutorID: "peer-1", ApplicationVersion: "v1"},
			},
		}
		transport := &mockTransport{
			errors: map[string]error{
				"peer-1": errors.New("timeout"),
			},
		}
		deleter := &mockDeleter{}

		dispatcher := liveness.NewRecoveryDispatcher(finder, transport, deleter, liveness.DispatcherOptions{})
		err := dispatcher.RecoverDeadExecutor(ctx, appID, "dead-1", "v1")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if len(deleter.deleted) != 0 {
			t.Errorf("expected dead record NOT deleted on failure, got %v", deleter.deleted)
		}
	})
}

type mockProbingTransport struct {
	mockTransport
	hasPending map[string]bool
	probeCalls []string
}

func (m *mockProbingTransport) CheckPendingWorkflows(ctx context.Context, appID pgtype.UUID, peerExecutorID, deadExecutorID, deadVersion string) (bool, error) {
	m.probeCalls = append(m.probeCalls, peerExecutorID)
	return m.hasPending[peerExecutorID], nil
}

type mockRecoveryRecorder struct {
	mu      sync.Mutex
	records []gen.RecordRecoveryDispatchParams
}

func (r *mockRecoveryRecorder) RecordRecoveryDispatch(ctx context.Context, arg gen.RecordRecoveryDispatchParams) (gen.RecoveryDispatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, arg)
	return gen.RecoveryDispatch{Success: arg.Success}, nil
}

func TestRecoverDeadExecutor_CrossVersionVerification(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	ctx := context.Background()

	t.Run("Aborts deletion when pending workflows exist", func(t *testing.T) {
		finder := &mockPeerFinder{
			peers: []liveness.Peer{
				{AppID: appID, ExecutorID: "peer-v2", ApplicationVersion: "v2"},
			},
		}
		transport := &mockProbingTransport{
			mockTransport: mockTransport{
				responses: map[string]*protocol.RecoveryResponse{
					"peer-v2": {Success: true},
				},
			},
			hasPending: map[string]bool{
				"peer-v2": true,
			},
		}
		deleter := &mockDeleter{}

		dispatcher := liveness.NewRecoveryDispatcher(finder, transport, deleter, liveness.DispatcherOptions{
			AllowVersionMismatch: true,
		})

		err := dispatcher.RecoverDeadExecutor(ctx, appID, "dead-v1", "v1")
		if err == nil {
			t.Fatal("expected error when pending workflows remain after cross-version recovery, got nil")
		}

		if len(deleter.deleted) != 0 {
			t.Fatalf("expected dead executor record NOT deleted, got: %v", deleter.deleted)
		}
	})

	t.Run("Deletes dead record when no pending workflows exist", func(t *testing.T) {
		finder := &mockPeerFinder{
			peers: []liveness.Peer{
				{AppID: appID, ExecutorID: "peer-v2", ApplicationVersion: "v2"},
			},
		}
		transport := &mockProbingTransport{
			mockTransport: mockTransport{
				responses: map[string]*protocol.RecoveryResponse{
					"peer-v2": {Success: true},
				},
			},
			hasPending: map[string]bool{
				"peer-v2": false,
			},
		}
		deleter := &mockDeleter{}

		dispatcher := liveness.NewRecoveryDispatcher(finder, transport, deleter, liveness.DispatcherOptions{
			AllowVersionMismatch: true,
		})

		err := dispatcher.RecoverDeadExecutor(ctx, appID, "dead-v1", "v1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(deleter.deleted) != 1 || deleter.deleted[0] != "dead-v1" {
			t.Fatalf("expected dead-v1 to be deleted, got: %v", deleter.deleted)
		}
	})
}

func TestRecoverDeadExecutor_RecorderCapturesBothSuccessAndFailure(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	ctx := context.Background()

	finder := &mockPeerFinder{
		peers: []liveness.Peer{
			{AppID: appID, ExecutorID: "peer-1", ApplicationVersion: "v1"},
			{AppID: appID, ExecutorID: "peer-2", ApplicationVersion: "v1"},
			{AppID: appID, ExecutorID: "peer-3", ApplicationVersion: "v1"},
		},
	}
	transport := &mockTransport{
		errors: map[string]error{
			"peer-1": errors.New("network failure"),
		},
		responses: map[string]*protocol.RecoveryResponse{
			"peer-2": {Success: false},
			"peer-3": {Success: true},
		},
	}
	deleter := &mockDeleter{}
	recorder := &mockRecoveryRecorder{}

	dispatcher := liveness.NewRecoveryDispatcher(finder, transport, deleter, liveness.DispatcherOptions{})
	dispatcher.SetRecorder(recorder)

	err := dispatcher.RecoverDeadExecutor(ctx, appID, "dead-1", "v1")
	if err != nil {
		t.Fatalf("expected recovery to succeed on peer-3, got %v", err)
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	if len(recorder.records) != 3 {
		t.Fatalf("expected 3 recorded dispatches, got %d", len(recorder.records))
	}

	if recorder.records[0].Success {
		t.Errorf("expected peer-1 dispatch recorded with success=false")
	}
	if recorder.records[1].Success {
		t.Errorf("expected peer-2 dispatch recorded with success=false")
	}
	if !recorder.records[2].Success {
		t.Errorf("expected peer-3 dispatch recorded with success=true")
	}
}

type mockDirectRecovery struct {
	has              bool
	ids              []string
	err              error
	pending          bool
	checkErr         error
	calls            int
	lastCheckVersion string
}

func (m *mockDirectRecovery) HasDirectRecovery(appID pgtype.UUID) bool { return m.has }
func (m *mockDirectRecovery) Recover(ctx context.Context, appID pgtype.UUID, executorIDs []string, appVersion string) ([]string, error) {
	m.calls++
	return m.ids, m.err
}
func (m *mockDirectRecovery) CheckPendingWorkflows(ctx context.Context, appID pgtype.UUID, executorID, appVersion string) (bool, error) {
	m.lastCheckVersion = appVersion
	return m.pending, m.checkErr
}

func TestRecoverDeadExecutor_DirectDatabasePath(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	ctx := context.Background()

	t.Run("database recovery deletes the dead executor", func(t *testing.T) {
		finder := &mockPeerFinder{err: errors.New("peer finder should not be called")}
		deleter := &mockDeleter{}
		recorder := &mockRecoveryRecorder{}
		direct := &mockDirectRecovery{has: true, ids: []string{"wf-1"}}
		d := liveness.NewRecoveryDispatcher(finder, &mockTransport{}, deleter, liveness.DispatcherOptions{})
		d.SetDirectRecovery(direct)
		d.SetRecorder(recorder)

		if err := d.RecoverDeadExecutor(ctx, appID, "exec-dead", "v1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if direct.calls != 1 {
			t.Fatalf("expected one direct recovery call, got %d", direct.calls)
		}
		if len(deleter.deleted) != 1 || deleter.deleted[0] != "exec-dead" {
			t.Fatalf("expected dead executor deletion, got %v", deleter.deleted)
		}
		if len(recorder.records) != 1 || recorder.records[0].TargetExecutorID != "database" {
			t.Fatalf("expected a database recovery record, got %+v", recorder.records)
		}
	})

	t.Run("database failure falls back to peers", func(t *testing.T) {
		finder := &mockPeerFinder{peers: []liveness.Peer{
			{AppID: appID, ExecutorID: "peer", ApplicationVersion: "v1"},
		}}
		transport := &mockTransport{responses: map[string]*protocol.RecoveryResponse{
			"peer": {Success: true},
		}}
		direct := &mockDirectRecovery{has: true, err: errors.New("read-only")}
		d := liveness.NewRecoveryDispatcher(finder, transport, &mockDeleter{}, liveness.DispatcherOptions{})
		d.SetDirectRecovery(direct)

		if err := d.RecoverDeadExecutor(ctx, appID, "exec-dead", "v1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if direct.calls != 1 {
			t.Fatalf("expected one direct attempt, got %d", direct.calls)
		}
		if len(transport.calls) != 1 || transport.calls[0] != "peer" {
			t.Fatalf("expected peer fallback, got %v", transport.calls)
		}
	})
}

func TestRecoverDeadExecutor_DirectPendingStopsDeletion(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	direct := &mockDirectRecovery{has: true, ids: []string{"wf-1"}, pending: true}
	deleter := &mockDeleter{}
	d := liveness.NewRecoveryDispatcher(&mockPeerFinder{}, &mockTransport{}, deleter, liveness.DispatcherOptions{})
	d.SetDirectRecovery(direct)

	err := d.RecoverDeadExecutor(context.Background(), appID, "exec-dead", "v1")
	if !errors.Is(err, liveness.ErrRecoveryFailed) {
		t.Fatalf("expected ErrRecoveryFailed, got %v", err)
	}
	if len(deleter.deleted) != 0 {
		t.Fatalf("dead executor must not be deleted while pending work remains: %v", deleter.deleted)
	}
	if direct.lastCheckVersion != "" {
		t.Fatalf("pending check must be version-agnostic, got version %q", direct.lastCheckVersion)
	}
}
