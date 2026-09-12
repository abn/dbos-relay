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

func TestManager_ConcurrentInitialization(t *testing.T) {
	appA := pgtype.UUID{Bytes: [16]byte{10, 1, 1}, Valid: true}
	appB := pgtype.UUID{Bytes: [16]byte{10, 1, 2}, Valid: true}

	blockA := make(chan struct{})
	factory := func(cfg dataplane.AppConfig) (dataplane.Client, error) {
		if cfg.ApplicationID == appA {
			<-blockA
		}
		return &mockClient{}, nil
	}

	mgr := dataplane.NewManager(factory)

	// App A initialization will block in factory; execute concurrently
	appARegistered := make(chan struct{})
	go func() {
		close(appARegistered)
		_ = mgr.RegisterApp(dataplane.AppConfig{ApplicationID: appA, DatabaseURL: "postgres://localhost/a"})
	}()
	<-appARegistered
	time.Sleep(10 * time.Millisecond)

	if err := mgr.RegisterApp(dataplane.AppConfig{ApplicationID: appB, DatabaseURL: "postgres://localhost/b"}); err != nil {
		t.Fatalf("register appB failed: %v", err)
	}

	appBDone := make(chan error, 1)
	go func() {
		readMsg := &protocol.ListWorkflowsRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-b"},
		}
		_, err := mgr.Dispatch(context.Background(), appB, readMsg)
		appBDone <- err
	}()

	select {
	case err := <-appBDone:
		if err != nil {
			t.Fatalf("appB dispatch failed: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("appB dispatch timed out while appA was initializing")
	}

	close(blockA)
}

func TestManager_NegativeCaching(t *testing.T) {
	appC := pgtype.UUID{Bytes: [16]byte{10, 1, 3}, Valid: true}
	factoryCalls := 0

	mgr := dataplane.NewManager(func(cfg dataplane.AppConfig) (dataplane.Client, error) {
		factoryCalls++
		return nil, errors.New("connection failed")
	})

	_ = mgr.RegisterApp(dataplane.AppConfig{ApplicationID: appC, DatabaseURL: "postgres://localhost/c"})

	readMsg := &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-c1"},
	}

	_, err1 := mgr.Dispatch(context.Background(), appC, readMsg)
	if err1 == nil {
		t.Fatalf("expected first dispatch to fail")
	}

	callsAfterFirst := factoryCalls

	_, err2 := mgr.Dispatch(context.Background(), appC, readMsg)
	if err2 == nil {
		t.Fatalf("expected second dispatch to fail")
	}

	if factoryCalls != callsAfterFirst {
		t.Fatalf("expected factory not to be called again during negative cache cooldown")
	}
}

func TestManager_AggregatesDispatch(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{10, 1, 4}, Valid: true}

	mock := &mockClient{
		onDispatch: func(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
			switch msg.GetMessageType() {
			case protocol.MessageTypeGetWorkflowAggregates:
				return &protocol.GetWorkflowAggregatesResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: "req-agg-1"},
				}, nil
			case protocol.MessageTypeGetStepAggregates:
				return &protocol.GetStepAggregatesResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeGetStepAggregates, RequestID: "req-agg-2"},
				}, nil
			default:
				return nil, errors.New("unsupported")
			}
		},
	}

	mgr := dataplane.NewManager(func(cfg dataplane.AppConfig) (dataplane.Client, error) {
		return mock, nil
	})

	if err := mgr.RegisterApp(dataplane.AppConfig{
		ApplicationID: appID,
		DatabaseURL:   "postgres://localhost/agg",
		Mode:          dataplane.ModeRead,
	}); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	wfAgg := &protocol.GetWorkflowAggregatesRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: "req-agg-1"},
	}
	res1, err := mgr.Dispatch(context.Background(), appID, wfAgg)
	if err != nil {
		t.Fatalf("dispatch workflow aggregates failed: %v", err)
	}
	if res1.GetMessageType() != protocol.MessageTypeGetWorkflowAggregates {
		t.Fatalf("expected MessageTypeGetWorkflowAggregates, got %s", res1.GetMessageType())
	}

	stepAgg := &protocol.GetStepAggregatesRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeGetStepAggregates, RequestID: "req-agg-2"},
	}
	res2, err := mgr.Dispatch(context.Background(), appID, stepAgg)
	if err != nil {
		t.Fatalf("dispatch step aggregates failed: %v", err)
	}
	if res2.GetMessageType() != protocol.MessageTypeGetStepAggregates {
		t.Fatalf("expected MessageTypeGetStepAggregates, got %s", res2.GetMessageType())
	}
}
