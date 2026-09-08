package dataplane_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/protocol"
)

type mockClient struct {
	onDispatch func(ctx context.Context, msg protocol.Message) (protocol.Message, error)
	closed     bool
}

func (m *mockClient) Dispatch(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
	if m.onDispatch != nil {
		return m.onDispatch(ctx, msg)
	}
	return nil, nil
}

func (m *mockClient) Close() error {
	m.closed = true
	return nil
}

func TestManager_LifecycleAndDispatch(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true}
	mock := &mockClient{
		onDispatch: func(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
			return &protocol.ListWorkflowsResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-1"},
			}, nil
		},
	}

	mgr := dataplane.NewManager(func(cfg dataplane.AppConfig) (dataplane.Client, error) {
		return mock, nil
	})

	// Before registration
	if mgr.HasDataPlane(appID) {
		t.Fatalf("expected HasDataPlane to be false before registration")
	}

	// Register with read-only mode
	err := mgr.RegisterApp(dataplane.AppConfig{
		ApplicationID: appID,
		DatabaseURL:   "postgres://localhost:5432/test",
		Mode:          dataplane.ModeRead,
	})
	if err != nil {
		t.Fatalf("RegisterApp failed: %v", err)
	}

	if !mgr.HasDataPlane(appID) {
		t.Fatalf("expected HasDataPlane to be true after registration")
	}

	mode, ok := mgr.GetMode(appID)
	if !ok || mode != dataplane.ModeRead {
		t.Fatalf("expected mode read, got %s", mode)
	}

	// Read dispatch succeeds
	readMsg := &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-1"},
	}
	res, err := mgr.Dispatch(context.Background(), appID, readMsg)
	if err != nil {
		t.Fatalf("expected read dispatch to succeed, got %v", err)
	}
	if res.GetMessageType() != protocol.MessageTypeListWorkflows {
		t.Fatalf("expected ListWorkflowsResponse, got %s", res.GetMessageType())
	}

	// Mutation dispatch in read-only mode fails with ErrReadOnlyMode
	mutMsg := &protocol.CancelWorkflowRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeCancel, RequestID: "req-2"},
	}
	_, err = mgr.Dispatch(context.Background(), appID, mutMsg)
	if !errors.Is(err, dataplane.ErrReadOnlyMode) {
		t.Fatalf("expected ErrReadOnlyMode, got %v", err)
	}

	// Re-register with read-write mode
	err = mgr.RegisterApp(dataplane.AppConfig{
		ApplicationID: appID,
		DatabaseURL:   "postgres://localhost:5432/test",
		Mode:          dataplane.ModeReadWrite,
	})
	if err != nil {
		t.Fatalf("re-register failed: %v", err)
	}

	mock.onDispatch = func(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
		return &protocol.CancelWorkflowResponse{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeCancel, RequestID: "req-2"},
			Success:  true,
		}, nil
	}

	res, err = mgr.Dispatch(context.Background(), appID, mutMsg)
	if err != nil {
		t.Fatalf("expected mutation to succeed in read-write mode, got %v", err)
	}
	if res.GetMessageType() != protocol.MessageTypeCancel {
		t.Fatalf("expected CancelWorkflowResponse, got %s", res.GetMessageType())
	}

	// Unregister shuts down client
	mgr.UnregisterApp(appID)
	if mgr.HasDataPlane(appID) {
		t.Fatalf("expected HasDataPlane to be false after unregister")
	}
	if !mock.closed {
		t.Fatalf("expected mock client to be closed on unregister")
	}
}

func TestManager_StatementTimeout(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{4, 5, 6}, Valid: true}
	slowMock := &mockClient{
		onDispatch: func(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
			select {
			case <-time.After(100 * time.Millisecond):
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	mgr := dataplane.NewManager(func(cfg dataplane.AppConfig) (dataplane.Client, error) {
		return slowMock, nil
	})

	err := mgr.RegisterApp(dataplane.AppConfig{
		ApplicationID:    appID,
		DatabaseURL:      "postgres://localhost:5432/test",
		StatementTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("RegisterApp failed: %v", err)
	}

	readMsg := &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-t"},
	}
	_, err = mgr.Dispatch(context.Background(), appID, readMsg)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}
