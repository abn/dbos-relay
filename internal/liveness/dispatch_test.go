package liveness_test

import (
	"context"
	"errors"
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
