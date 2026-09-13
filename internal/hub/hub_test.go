package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/fakeexecutor"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

func TestHandleAuthError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{
			name:       "invalid conductor key",
			err:        errors.New("invalid conductor key"),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing app name or conductor key",
			err:        errors.New("missing app name or conductor key"),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "key does not have access to this application",
			err:        errors.New("key does not have access to this application"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "generic error",
			err:        errors.New("some unexpected database error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			HandleAuthError(rec, req, tt.err)
			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestMultiplexer_LateResponse(t *testing.T) {
	m := NewMultiplexer()
	ch, unregister, err := m.Register("req1")
	if err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	// Simulate timeout
	unregister()

	// Late response should not panic
	routed := m.RouteResponse(&protocol.Envelope{RequestID: "req1"})
	if routed {
		t.Error("expected false, got true")
	}

	// Read from channel should yield nothing and be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed")
		}
	default:
		t.Error("expected channel to be closed")
	}
}

func TestMultiplexer_DuplicateRequestID(t *testing.T) {
	m := NewMultiplexer()
	ch1, unreg1, err := m.Register("req-dup")
	if err != nil {
		t.Fatalf("expected first register to succeed, got: %v", err)
	}
	defer unreg1()

	// Second registration of same request ID must return error
	ch2, unreg2, err := m.Register("req-dup")
	if err == nil {
		unreg2()
		t.Fatal("expected duplicate register to fail with error, got nil")
	}
	if ch2 != nil {
		t.Errorf("expected nil channel on duplicate register, got %v", ch2)
	}

	// First channel must remain open and routeable
	routed := m.RouteResponse(&protocol.Envelope{RequestID: "req-dup"})
	if !routed {
		t.Fatal("expected message to route to first channel")
	}

	select {
	case msg, ok := <-ch1:
		if !ok || msg == nil {
			t.Fatal("expected valid message on first channel")
		}
	default:
		t.Fatal("expected message ready on first channel")
	}
}

func TestMultiplexer_UnregisterIdentityBound(t *testing.T) {
	m := NewMultiplexer()
	ch1, unreg1, err := m.Register("req-reuse")
	if err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}
	_ = ch1

	// Unregister first waiter
	unreg1()

	// Register second waiter with same ID
	ch2, unreg2, err := m.Register("req-reuse")
	if err != nil {
		t.Fatalf("second register failed: %v", err)
	}
	defer unreg2()

	// Calling stale unreg1 again must NOT delete or close ch2
	unreg1()

	select {
	case _, ok := <-ch2:
		if !ok {
			t.Fatal("stale unregister closed the second channel")
		}
	default:
		// Expected: channel is still open
	}

	// Channel 2 should still be routable
	routed := m.RouteResponse(&protocol.Envelope{RequestID: "req-reuse"})
	if !routed {
		t.Fatal("expected RouteResponse to succeed for second channel")
	}

	select {
	case msg, ok := <-ch2:
		if !ok || msg == nil {
			t.Fatal("expected valid message on second channel")
		}
	default:
		t.Fatal("expected message ready on second channel")
	}
}

type mockHubStore struct {
	memoryAuthStore
	disconnected      []gen.DisconnectExecutorParams
	disconnectCtxErrs []error
	upserted          []gen.UpsertExecutorParams
	touches           []gen.TouchExecutorLastSeenParams
	executors         map[string]gen.Executor
	mu                sync.Mutex
}

func newMockHubStore() *mockHubStore {
	store := &mockHubStore{
		memoryAuthStore: *newMemoryAuthStore(),
		executors:       make(map[string]gen.Executor),
	}
	lookup := auth.Lookup("test-key")
	store.keys[lookup] = gen.ApiKey{
		ID:               pgtype.UUID{Bytes: [16]byte{1, 1, 1, 1}, Valid: true},
		OrganisationID:   pgtype.UUID{Bytes: [16]byte{9, 9, 9, 9}, Valid: true},
		Lookup:           lookup,
		KeyHash:          auth.Hash("test-key"),
		ApplicationNames: []string{},
	}
	return store
}

func (m *mockHubStore) UpsertExecutor(ctx context.Context, arg gen.UpsertExecutorParams) (gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upserted = append(m.upserted, arg)
	ex := gen.Executor{
		ExecutorID:      arg.ExecutorID,
		Status:          "connected",
		OwnerInstanceID: arg.OwnerInstanceID,
		LeaseExpiresAt:  arg.LeaseExpiresAt,
	}
	m.executors[arg.ExecutorID] = ex
	return ex, nil
}

func (m *mockHubStore) DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnected = append(m.disconnected, arg)
	m.disconnectCtxErrs = append(m.disconnectCtxErrs, ctx.Err())
	ex := m.executors[arg.ExecutorID]
	ex.Status = "disconnected"
	m.executors[arg.ExecutorID] = ex
	return ex, nil
}

func (m *mockHubStore) TouchExecutorLastSeen(ctx context.Context, arg gen.TouchExecutorLastSeenParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touches = append(m.touches, arg)
	return nil
}

func TestHub_HandshakeOrder(t *testing.T) {
	t.Log("executor is internal/fakeexecutor protocol stand-in, not a real DBOS SDK")
	store := newMockHubStore()
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	// Verify Relay's first frame is an executor_info request
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read first frame from relay: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("expected text message, got %v", typ)
	}
	msg, err := protocol.Decode(data)
	if err != nil {
		t.Fatalf("failed to decode message: %v", err)
	}
	infoReq, ok := msg.(*protocol.ExecutorInfoRequest)
	if !ok {
		t.Fatalf("expected *protocol.ExecutorInfoRequest, got %T", msg)
	}
	if infoReq.RequestID == "" {
		t.Fatalf("expected non-empty request id in executor_info request")
	}

	// Echo request id in response
	infoResp := &protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: infoReq.RequestID,
		},
		ExecutorID:         "test-exec-1",
		ApplicationVersion: "v1.0.0",
	}
	respData, err := protocol.Encode(infoResp)
	if err != nil {
		t.Fatalf("failed to encode executor_info response: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
		t.Fatalf("failed to write executor_info response: %v", err)
	}

	// Verify executor was registered in hub
	time.Sleep(100 * time.Millisecond)
	connected := h.registry.ListConnected(store.apps["test-app"].ID)
	// Even if app is nil UUID in no-auth mode, registry can be checked directly
	found := false
	for _, appConns := range h.registry.byApp {
		if _, ok := appConns["test-exec-1"]; ok {
			found = true
			break
		}
	}
	if !found && len(connected) == 0 {
		t.Fatalf("expected executor test-exec-1 to be registered in hub registry")
	}
}

func TestHub_HandshakeTimeout(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()
	h.SetHandshakeTimeout(100 * time.Millisecond)

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"

	// Open 25 upgrades that stall (never send executor_info)
	const numClients = 25
	conns := make([]*websocket.Conn, numClients)
	ctx := context.Background()

	for i := 0; i < numClients; i++ {
		c, resp, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("failed to dial client %d: %v", i, err)
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		conns[i] = c
		defer func(conn *websocket.Conn) { _ = conn.Close(websocket.StatusNormalClosure, "") }(c)
	}

	// Wait past the handshake timeout (100ms timeout + buffer)
	time.Sleep(250 * time.Millisecond)

	// Assert all 25 sockets were closed by the server with StatusPolicyViolation.
	// If the server handshake timeout is missing or broken, c.Read times out with DeadlineExceeded.
	for i, c := range conns {
		readCtx, readCancel := context.WithTimeout(ctx, 1*time.Second)
		// Drain the initial prompt frame sent by server if not yet read
		_, _, err := c.Read(readCtx)
		if err == nil {
			_, _, err = c.Read(readCtx)
		}
		readCancel()
		if errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("client %d timed out waiting for server to close socket: server never closed stalled connection", i)
		}
		if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
			t.Fatalf("client %d expected StatusPolicyViolation (%d), got err=%v (close status %d)",
				i, websocket.StatusPolicyViolation, err, websocket.CloseStatus(err))
		}
	}

	// Wait a moment for handler goroutines to unwind after observing connection closure
	time.Sleep(50 * time.Millisecond)

	// Assert runtime stack has no Hub.ServeHTTP frames lingering after close
	buf := make([]byte, 1024*1024)
	n := runtime.Stack(buf, true)
	stack := string(buf[:n])
	if strings.Contains(stack, "(*Hub).ServeHTTP") || strings.Contains(stack, "hub.ServeHTTP") {
		t.Fatalf("expected no lingering Hub.ServeHTTP goroutines, but found in stack: %s", stack)
	}
}

func TestHub_HandshakeRejection(t *testing.T) {
	runTest := func(t *testing.T, respModifier func(req *protocol.ExecutorInfoRequest) *protocol.ExecutorInfoResponse) {
		store := newMockHubStore()
		cfg := &config.Config{}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		h := New(store, cfg, logger)
		defer func() { _ = h.Close() }()

		server := httptest.NewServer(h)
		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, resp, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("failed to dial websocket: %v", err)
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

		// Read executor_info prompt from relay
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("failed to read prompt: %v", err)
		}
		if typ != websocket.MessageText {
			t.Fatalf("expected text message, got %v", typ)
		}
		msg, err := protocol.Decode(data)
		if err != nil {
			t.Fatalf("failed to decode prompt: %v", err)
		}
		infoReq, ok := msg.(*protocol.ExecutorInfoRequest)
		if !ok {
			t.Fatalf("expected *protocol.ExecutorInfoRequest, got %T", msg)
		}

		errResp := respModifier(infoReq)
		respData, err := protocol.Encode(errResp)
		if err != nil {
			t.Fatalf("failed to encode response: %v", err)
		}
		if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}

		// Connection must be closed by server with policy violation
		_, _, err = conn.Read(ctx)
		if err == nil {
			t.Fatalf("expected connection to be closed by server, but read succeeded")
		}
		var closeErr websocket.CloseError
		if errors.As(err, &closeErr) {
			if closeErr.Code != websocket.StatusPolicyViolation {
				t.Errorf("expected close code StatusPolicyViolation (%d), got %d", websocket.StatusPolicyViolation, closeErr.Code)
			}
		} else if !strings.Contains(err.Error(), "StatusPolicyViolation") && !strings.Contains(err.Error(), "1008") {
			t.Errorf("expected StatusPolicyViolation or code 1008 in error, got: %v", err)
		}

		// Verify no executors registered in hub
		connected := h.registry.ListConnected(store.apps["test-app"].ID)
		if len(connected) != 0 {
			t.Errorf("expected 0 connected executors, got %d", len(connected))
		}
	}

	t.Run("error_message rejected", func(t *testing.T) {
		errMsg := "boom"
		runTest(t, func(req *protocol.ExecutorInfoRequest) *protocol.ExecutorInfoResponse {
			return &protocol.ExecutorInfoResponse{
				Envelope: protocol.Envelope{
					Type:         protocol.MessageTypeExecutorInfo,
					RequestID:    req.RequestID,
					ErrorMessage: &errMsg,
				},
			}
		})
	})

	t.Run("empty_executor_id rejected", func(t *testing.T) {
		runTest(t, func(req *protocol.ExecutorInfoRequest) *protocol.ExecutorInfoResponse {
			return &protocol.ExecutorInfoResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeExecutorInfo,
					RequestID: req.RequestID,
				},
				ExecutorID: "",
			}
		})
	})
}

func TestHub_ReadLimit_LargePayload(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	// Read executor_info prompt from relay
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read prompt: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("expected text message, got %v", typ)
	}
	msg, err := protocol.Decode(data)
	if err != nil {
		t.Fatalf("failed to decode prompt: %v", err)
	}
	infoReq, ok := msg.(*protocol.ExecutorInfoRequest)
	if !ok {
		t.Fatalf("expected *protocol.ExecutorInfoRequest, got %T", msg)
	}

	// Send executor_info response
	infoResp := &protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: infoReq.RequestID,
		},
		ExecutorID:         "large-payload-exec",
		ApplicationVersion: "v1.0.0",
	}
	respData, err := protocol.Encode(infoResp)
	if err != nil {
		t.Fatalf("failed to encode response: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
		t.Fatalf("failed to write response: %v", err)
	}

	// Wait for executor to register
	time.Sleep(100 * time.Millisecond)

	appID := store.apps["test-app"].ID
	execConn, err := h.registry.GetExecutorConn(appID, "large-payload-exec")
	if err != nil {
		t.Fatalf("expected executor to be registered: %v", err)
	}

	// Register a request ID in the executor conn multiplexer
	reqID := "req-large-payload-1"
	ch, unreg, err := execConn.mux.Register(reqID)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	defer unreg()

	// Construct a >32 KiB (64 KiB) response payload
	largeInput := strings.Repeat("A", 64*1024)
	largeMsg := &protocol.ListWorkflowsResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListWorkflows,
			RequestID: reqID,
		},
		Output: []protocol.ListWorkflowsResponseBody{
			{
				WorkflowUUID: "wf-large-1",
				Input:        &largeInput,
			},
		},
	}
	largeData, err := protocol.Encode(largeMsg)
	if err != nil {
		t.Fatalf("failed to encode large message: %v", err)
	}
	if len(largeData) <= 32768 {
		t.Fatalf("test payload must be > 32 KiB, got %d bytes", len(largeData))
	}

	// Client sends large frame across websocket
	if err := conn.Write(ctx, websocket.MessageText, largeData); err != nil {
		t.Fatalf("failed to write large payload: %v", err)
	}

	// Wait for multiplexer to receive the response intact
	select {
	case receivedMsg := <-ch:
		listResp, ok := receivedMsg.(*protocol.ListWorkflowsResponse)
		if !ok {
			t.Fatalf("expected *protocol.ListWorkflowsResponse, got %T", receivedMsg)
		}
		if len(listResp.Output) != 1 || listResp.Output[0].Input == nil || len(*listResp.Output[0].Input) != 64*1024 {
			t.Fatalf("payload was corrupted or incomplete")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for large message to be routed through multiplexer")
	}

	// Assert connection is still registered and resolves
	resolvedConn, err := h.registry.GetExecutorConn(appID, "large-payload-exec")
	if err != nil {
		t.Fatalf("expected connection to still be registered after reading >32 KiB message: %v", err)
	}
	if resolvedConn != execConn {
		t.Fatalf("expected resolved connection to match original connection")
	}
}

func TestHub_Heartbeat_PongTimeoutAndUnregister(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{
		ExecutorDeadline: 50 * time.Millisecond,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()
	// Configure fast ping interval (20ms) and pong timeout (40ms)
	h.SetPingPongTimeouts(20*time.Millisecond, 40*time.Millisecond)

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Dial with OnPingReceived returning false (never pong)
	dialOpts := &websocket.DialOptions{
		OnPingReceived: func(ctx context.Context, payload []byte) bool {
			return false // Do not auto-pong!
		},
	}
	conn, resp, err := websocket.Dial(ctx, wsURL, dialOpts)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	// Read executor_info prompt from relay
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read prompt: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("expected text message, got %v", typ)
	}
	msg, err := protocol.Decode(data)
	if err != nil {
		t.Fatalf("failed to decode prompt: %v", err)
	}
	infoReq, ok := msg.(*protocol.ExecutorInfoRequest)
	if !ok {
		t.Fatalf("expected *protocol.ExecutorInfoRequest, got %T", msg)
	}

	// Send executor_info response
	infoResp := &protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: infoReq.RequestID,
		},
		ExecutorID:         "unresponsive-peer-1",
		ApplicationVersion: "v1.0.0",
	}
	respData, err := protocol.Encode(infoResp)
	if err != nil {
		t.Fatalf("failed to encode response: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
		t.Fatalf("failed to write response: %v", err)
	}

	// Wait for registration
	time.Sleep(10 * time.Millisecond)

	appID := store.apps["test-app"].ID
	execConn, err := h.registry.GetExecutorConn(appID, "unresponsive-peer-1")
	if err != nil {
		t.Fatalf("expected executor to be registered: %v", err)
	}

	// WriteMessage must not block forever when peer doesn't pong
	writeCtx, writeCancel := context.WithTimeout(ctx, 30*time.Millisecond)
	_ = execConn.WriteMessage(writeCtx, &protocol.RecoveryRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeRecovery,
			RequestID: "test-write",
		},
		ExecutorIDs: []string{"unresponsive-peer-1"},
	})
	writeCancel()

	// Wait for pingInterval + pongTimeout to elapse (20ms + 40ms = 60ms; wait 150ms)
	time.Sleep(150 * time.Millisecond)

	// Verify executor is unregistered from registry
	_, err = h.registry.GetExecutorConn(appID, "unresponsive-peer-1")
	if err == nil {
		t.Fatalf("expected executor to be unregistered after pong timeout")
	}

	// Verify DisconnectExecutor was recorded in store
	store.mu.Lock()
	discCount := len(store.disconnected)
	store.mu.Unlock()
	if discCount == 0 {
		t.Fatalf("expected DisconnectExecutor to be called, got 0")
	}
}

func TestHub_Dispatch_TimeoutAndCancelledContext(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{
		ExecutorDeadline: 50 * time.Millisecond,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	// Handshake
	typ, data, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("read prompt failed: %v", err)
	}
	msg, _ := protocol.Decode(data)
	infoReq := msg.(*protocol.ExecutorInfoRequest)

	infoResp := &protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: infoReq.RequestID,
		},
		ExecutorID:         "dispatch-test-exec",
		ApplicationVersion: "v1.0.0",
	}
	respData, _ := protocol.Encode(infoResp)
	_ = conn.Write(ctx, websocket.MessageText, respData)

	time.Sleep(10 * time.Millisecond)

	appID := store.apps["test-app"].ID

	// 1. Dispatch with an already-cancelled context returns immediately
	cancelledCtx, cancelNow := context.WithCancel(ctx)
	cancelNow()
	_, err = h.Dispatch(cancelledCtx, appID, &protocol.RecoveryRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeRecovery,
			RequestID: "req-cancelled",
		},
		ExecutorIDs: []string{"dispatch-test-exec"},
	})
	if err == nil {
		t.Fatalf("expected error on cancelled context dispatch, got nil")
	}

	// 2. Dispatch to an executor that does not respond times out within ExecutorDeadline
	start := time.Now()
	_, err = h.Dispatch(ctx, appID, &protocol.RecoveryRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeRecovery,
			RequestID: "req-silent",
		},
		ExecutorIDs: []string{"dispatch-test-exec"},
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected dispatch timeout error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("dispatch took too long: %v, expected ~50ms", elapsed)
	}
}

func TestHub_Reconnect_EvictionPrevention(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connectExecutor := func(id string) *websocket.Conn {
		conn, resp, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("failed to dial: %v", err)
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}

		typ, data, err := conn.Read(ctx)
		if err != nil || typ != websocket.MessageText {
			t.Fatalf("failed to read prompt: %v", err)
		}
		msg, _ := protocol.Decode(data)
		infoReq := msg.(*protocol.ExecutorInfoRequest)

		infoResp := &protocol.ExecutorInfoResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeExecutorInfo,
				RequestID: infoReq.RequestID,
			},
			ExecutorID:         id,
			ApplicationVersion: "v1.0.0",
		}
		respData, _ := protocol.Encode(infoResp)
		if err := conn.Write(ctx, websocket.MessageText, respData); err != nil {
			t.Fatalf("failed to write info response: %v", err)
		}
		return conn
	}

	// 1. Connect Executor A
	connA := connectExecutor("exec-reconnect-1")
	defer func() { _ = connA.Close(websocket.StatusNormalClosure, "done") }()

	time.Sleep(50 * time.Millisecond)
	appID := store.apps["test-app"].ID
	initialConn, err := h.registry.GetExecutorConn(appID, "exec-reconnect-1")
	if err != nil {
		t.Fatalf("expected executor A to be registered: %v", err)
	}

	// 2. Connect Executor B with same executorID
	connB := connectExecutor("exec-reconnect-1")
	defer func() { _ = connB.Close(websocket.StatusNormalClosure, "done") }()

	time.Sleep(50 * time.Millisecond)

	// Close Executor A
	_ = connA.Close(websocket.StatusNormalClosure, "replaced")
	time.Sleep(50 * time.Millisecond)

	// 3. Verify B is still registered and active in the registry
	currentConn, err := h.registry.GetExecutorConn(appID, "exec-reconnect-1")
	if err != nil {
		t.Fatalf("expected executor B to still be registered after A closed: %v", err)
	}
	if currentConn == initialConn {
		t.Fatalf("expected current connection to be B, not initial connection A")
	}

	connected := h.registry.ListConnected(appID)
	if len(connected) != 1 {
		t.Fatalf("expected exactly 1 connected executor, got %d", len(connected))
	}

	// 4. Verify DisconnectExecutor was NOT called for the active executor
	store.mu.Lock()
	discCalls := len(store.disconnected)
	store.mu.Unlock()
	if discCalls != 0 {
		t.Fatalf("expected DisconnectExecutor not to be called for active executor, got %d calls", discCalls)
	}
}

func TestExecutorConn_Close_Idempotent(t *testing.T) {
	unregCount := 0
	var mu sync.Mutex
	unreg := func() {
		mu.Lock()
		defer mu.Unlock()
		unregCount++
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	clientConn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = clientConn.Close(websocket.StatusNormalClosure, "") }()

	execConn := NewExecutorConn(clientConn, pgtype.UUID{}, "exec-1", "app-1", "v1", "host-1", nil, NewMultiplexer(), unreg)

	// Call Close concurrently 10 times
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = execConn.Close()
		}()
	}
	wg.Wait()

	mu.Lock()
	calls := unregCount
	mu.Unlock()

	if calls != 1 {
		t.Fatalf("expected unregister to be called exactly once, got %d", calls)
	}
}

func TestHub_Handshake_InvalidIdentifiers(t *testing.T) {
	testCases := []struct {
		name       string
		executorID string
		hostname   *string
	}{
		{
			name:       "executor_id with newline control character",
			executorID: "exec\nid",
		},
		{
			name:       "executor_id with null byte",
			executorID: "exec\x00id",
		},
		{
			name:       "executor_id over 255 chars",
			executorID: strings.Repeat("x", 256),
		},
		{
			name:       "hostname with control character",
			executorID: "valid-exec-id",
			hostname: func() *string {
				s := "host\x01name"
				return &s
			}(),
		},
		{
			name:       "hostname over 255 chars",
			executorID: "valid-exec-id",
			hostname: func() *string {
				s := strings.Repeat("h", 256)
				return &s
			}(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := newMockHubStore()
			cfg := &config.Config{}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			h := New(store, cfg, logger)
			defer func() { _ = h.Close() }()

			server := httptest.NewServer(h)
			defer server.Close()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, resp, err := websocket.Dial(ctx, wsURL, nil)
			if err != nil {
				t.Fatalf("failed to dial websocket: %v", err)
			}
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

			// Read prompt
			typ, data, err := conn.Read(ctx)
			if err != nil || typ != websocket.MessageText {
				t.Fatalf("failed to read prompt: %v", err)
			}
			msg, _ := protocol.Decode(data)
			infoReq := msg.(*protocol.ExecutorInfoRequest)

			infoResp := &protocol.ExecutorInfoResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeExecutorInfo,
					RequestID: infoReq.RequestID,
				},
				ExecutorID:         tc.executorID,
				ApplicationVersion: "v1.0.0",
				Hostname:           tc.hostname,
			}
			respData, _ := protocol.Encode(infoResp)
			_ = conn.Write(ctx, websocket.MessageText, respData)

			// Server must close with StatusPolicyViolation
			_, _, err = conn.Read(ctx)
			if err == nil {
				t.Fatalf("expected server to close connection, but read succeeded")
			}
			var closeErr websocket.CloseError
			if errors.As(err, &closeErr) {
				if closeErr.Code != websocket.StatusPolicyViolation {
					t.Errorf("expected StatusPolicyViolation (%d), got %d", websocket.StatusPolicyViolation, closeErr.Code)
				}
			} else if !strings.Contains(err.Error(), "StatusPolicyViolation") && !strings.Contains(err.Error(), "1008") {
				t.Errorf("expected StatusPolicyViolation or code 1008, got: %v", err)
			}
		})
	}
}

func TestHub_Close_DeadlockSafety(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()
		for {
			if _, _, err := c.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	clientConn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = clientConn.Close(websocket.StatusNormalClosure, "") }()

	var execConn *ExecutorConn
	unreg := func() {
		if execConn != nil {
			h.registry.Unregister(h.ctx, execConn)
		}
	}

	appID := pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true}
	execConn = NewExecutorConn(clientConn, appID, "deadlock-exec-1", "test-app", "v1", "host-1", nil, NewMultiplexer(), unreg)
	h.registry.Register(execConn)

	// Calling Close when a connection is still registered must not deadlock
	done := make(chan error, 1)
	go func() {
		done <- h.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error from Hub.Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Hub.Close deadlocked while closing registered connection")
	}

	if conns := h.registry.ListConnected(appID); len(conns) != 0 {
		t.Fatalf("expected registry to be empty after Close, got %d connections", len(conns))
	}
}

func TestHub_Close_DisconnectWritesNotCancelled(t *testing.T) {
	t.Log("executor is internal/fakeexecutor protocol stand-in, not a real DBOS SDK")
	store := newMockHubStore()
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)

	server := httptest.NewServer(h)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fe := fakeexecutor.New(fakeexecutor.Options{
		URL:          server.URL,
		AppName:      "test-app",
		ConductorKey: "test-key",
		ExecutorID:   "exec-shutdown-disconnect",
	})
	if err := fe.Connect(ctx); err != nil {
		t.Fatalf("failed to connect fakeexecutor: %v", err)
	}

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- fe.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	appID := store.apps["test-app"].ID
	if _, err := h.registry.GetExecutorConn(appID, "exec-shutdown-disconnect"); err != nil {
		t.Fatalf("expected executor to be registered: %v", err)
	}

	// Trigger Hub.Close
	if err := h.Close(); err != nil {
		t.Fatalf("Hub.Close failed: %v", err)
	}

	var runErr error
	select {
	case runErr = <-runErrCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fakeexecutor.Run to exit after Hub.Close")
	}

	if websocket.CloseStatus(runErr) != websocket.StatusNormalClosure {
		t.Fatalf("expected websocket StatusNormalClosure (%d), got error: %v (close status %d)",
			websocket.StatusNormalClosure, runErr, websocket.CloseStatus(runErr))
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.disconnected) != 1 {
		t.Fatalf("expected DisconnectExecutor to be called exactly once, got %d", len(store.disconnected))
	}

	if store.executors["exec-shutdown-disconnect"].Status != "disconnected" {
		t.Fatalf("expected executor status to be 'disconnected', got %q", store.executors["exec-shutdown-disconnect"].Status)
	}

	for i, ctxErr := range store.disconnectCtxErrs {
		if ctxErr != nil {
			t.Fatalf("DisconnectExecutor call %d failed with cancelled context: %v", i, ctxErr)
		}
	}
}

func TestHub_Dispatch_RetryOnSilentExecutor(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{
		ExecutorDeadline: 20 * time.Millisecond,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()

	server := httptest.NewServer(h)
	defer server.Close()

	wsBase := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect silent executor
	silentConn, resp, err := websocket.Dial(ctx, wsBase, nil)
	if err != nil {
		t.Fatalf("failed to dial silent executor: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = silentConn.Close(websocket.StatusNormalClosure, "done") }()

	typ, data, err := silentConn.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("silent executor read prompt failed: %v", err)
	}
	msg, _ := protocol.Decode(data)
	infoReq := msg.(*protocol.ExecutorInfoRequest)
	infoResp := &protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: infoReq.RequestID,
		},
		ExecutorID:         "exec-silent",
		ApplicationVersion: "v1.0.0",
	}
	respBytes, _ := protocol.Encode(infoResp)
	_ = silentConn.Write(ctx, websocket.MessageText, respBytes)

	// Read and discard loop for silent executor
	go func() {
		for {
			_, _, readErr := silentConn.Read(ctx)
			if readErr != nil {
				return
			}
		}
	}()

	// Connect active executor
	activeConn, resp, err := websocket.Dial(ctx, wsBase, nil)
	if err != nil {
		t.Fatalf("failed to dial active executor: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = activeConn.Close(websocket.StatusNormalClosure, "done") }()

	typ, data, err = activeConn.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("active executor read prompt failed: %v", err)
	}
	msg, _ = protocol.Decode(data)
	infoReq = msg.(*protocol.ExecutorInfoRequest)
	infoResp = &protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: infoReq.RequestID,
		},
		ExecutorID:         "exec-active",
		ApplicationVersion: "v1.0.0",
	}
	respBytes, _ = protocol.Encode(infoResp)
	_ = activeConn.Write(ctx, websocket.MessageText, respBytes)

	// Responder loop for active executor
	go func() {
		for {
			readTyp, readData, readErr := activeConn.Read(ctx)
			if readErr != nil {
				return
			}
			if readTyp != websocket.MessageText {
				continue
			}
			reqMsg, decodeErr := protocol.DecodeRequest(readData)
			if decodeErr != nil {
				continue
			}
			activeResp := &protocol.ListWorkflowsResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeListWorkflows,
					RequestID: reqMsg.GetRequestID(),
				},
				Output: []protocol.ListWorkflowsResponseBody{},
			}
			activeBytes, _ := protocol.Encode(activeResp)
			_ = activeConn.Write(ctx, websocket.MessageText, activeBytes)
		}
	}()

	time.Sleep(50 * time.Millisecond)

	appID := store.apps["test-app"].ID

	// 20 dispatches must all succeed despite 1 of 2 executors being silent
	for i := 0; i < 20; i++ {
		req := &protocol.ListWorkflowsRequest{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListWorkflows,
				RequestID: fmt.Sprintf("req-retry-%d", i),
			},
		}
		res, err := h.Dispatch(ctx, appID, req)
		if err != nil {
			t.Fatalf("dispatch %d failed: %v", i, err)
		}
		if res == nil {
			t.Fatalf("dispatch %d returned nil response", i)
		}
	}
}

func TestHub_Dispatch_SingleExecutorTimeout(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{
		ExecutorDeadline: 20 * time.Millisecond,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	typ, data, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("read prompt failed: %v", err)
	}
	msg, _ := protocol.Decode(data)
	infoReq := msg.(*protocol.ExecutorInfoRequest)
	infoResp := &protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: infoReq.RequestID,
		},
		ExecutorID:         "exec-silent-alone",
		ApplicationVersion: "v1.0.0",
	}
	respData, _ := protocol.Encode(infoResp)
	_ = conn.Write(ctx, websocket.MessageText, respData)

	go func() {
		for {
			_, _, readErr := conn.Read(ctx)
			if readErr != nil {
				return
			}
		}
	}()

	time.Sleep(30 * time.Millisecond)

	appID := store.apps["test-app"].ID
	req := &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListWorkflows,
			RequestID: "req-alone-timeout",
		},
	}
	_, err = h.Dispatch(ctx, appID, req)
	if err == nil {
		t.Fatal("expected dispatch timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected error containing 'timed out', got %v", err)
	}
}

func TestHub_LeaseOwnershipAndRenewal(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()

	localInstanceID := pgtype.UUID{Bytes: [16]byte{42, 42, 42, 42}, Valid: true}
	h.SetInstanceID(localInstanceID)
	h.SetPingPongTimeouts(20*time.Millisecond, 50*time.Millisecond)

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	typ, data, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		t.Fatalf("read prompt failed: %v", err)
	}
	msg, _ := protocol.Decode(data)
	infoReq := msg.(*protocol.ExecutorInfoRequest)
	infoResp := &protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: infoReq.RequestID,
		},
		ExecutorID:         "exec-lease-test",
		ApplicationVersion: "v1.0.0",
	}
	respData, _ := protocol.Encode(infoResp)
	_ = conn.Write(ctx, websocket.MessageText, respData)

	// Keep conn open to allow ping/pong loop to run
	go func() {
		for {
			_, _, readErr := conn.Read(ctx)
			if readErr != nil {
				return
			}
		}
	}()

	time.Sleep(60 * time.Millisecond)

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.upserted) == 0 {
		t.Fatal("expected UpsertExecutor to be called")
	}
	lastUpsert := store.upserted[len(store.upserted)-1]
	if !lastUpsert.OwnerInstanceID.Valid || lastUpsert.OwnerInstanceID != localInstanceID {
		t.Fatalf("expected OwnerInstanceID %v, got %v", localInstanceID, lastUpsert.OwnerInstanceID)
	}
	if !lastUpsert.LeaseExpiresAt.Valid {
		t.Fatal("expected LeaseExpiresAt to be valid")
	}

	if len(store.touches) == 0 {
		t.Fatal("expected TouchExecutorLastSeen to be called during heartbeat renewal")
	}
	lastTouch := store.touches[len(store.touches)-1]
	if lastTouch.ExecutorID != "exec-lease-test" {
		t.Fatalf("expected touch for exec-lease-test, got %s", lastTouch.ExecutorID)
	}
	if !lastTouch.LeaseExpiresAt.Valid {
		t.Fatal("expected touch LeaseExpiresAt to be valid")
	}
}

func TestHub_ReadPump_ZeroValueResponse(t *testing.T) {
	store := newMockHubStore()
	cfg := &config.Config{
		ExecutorDeadline: 5 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(store, cfg, logger)
	defer func() { _ = h.Close() }()

	server := httptest.NewServer(h)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/websocket/test-app/test-key"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()

	// Read executor_info request from Relay
	_, _, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read executor_info request: %v", err)
	}

	// Send executor_info response
	infoResp := protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: "handshake-req",
		},
		ExecutorID:         "exec-zero-val",
		ApplicationVersion: "1.0.0",
	}
	infoBytes, err := protocol.Encode(&infoResp)
	if err != nil {
		t.Fatalf("failed to encode handshake response: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, infoBytes); err != nil {
		t.Fatalf("failed to write handshake response: %v", err)
	}

	// Give registration a moment to complete
	time.Sleep(50 * time.Millisecond)

	// In a goroutine, respond to the dispatch with a bare frame
	go func() {
		readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer readCancel()
		_, data, err := conn.Read(readCtx)
		if err != nil {
			return
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return
		}
		var reqID string
		_ = json.Unmarshal(raw["request_id"], &reqID)

		// Send zero-value get_workflow response frame
		bareResp := fmt.Sprintf(`{"type":"get_workflow","request_id":"%s"}`, reqID)
		_ = conn.Write(readCtx, websocket.MessageText, []byte(bareResp))
	}()

	dispatchReq := &protocol.GetWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflow,
			RequestID: "req-wf-zero",
		},
		WorkflowID: "non-existent-wf",
	}
	appID := store.apps["test-app"].ID
	dispatchResp, err := h.Dispatch(ctx, appID, dispatchReq)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	wfResp, ok := dispatchResp.(*protocol.GetWorkflowResponse)
	if !ok {
		t.Fatalf("expected *protocol.GetWorkflowResponse, got %T", dispatchResp)
	}
	if wfResp.Output != nil {
		t.Errorf("expected Output to be nil, got %v", wfResp.Output)
	}
}

func TestHub_ForkNotRetriedOnTimeout(t *testing.T) {
	forkReq := &protocol.ForkWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeForkWorkflow,
			RequestID: "req-fork-timeout",
		},
		Body: protocol.ForkWorkflowRequestBody{
			WorkflowID: "wf-123",
		},
	}
	if isSafeToRetry(forkReq) {
		t.Fatalf("expected ForkWorkflowRequest to not be safe to retry on timeout")
	}

	forkFailReq := &protocol.ForkFromFailureRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeForkFromFailure,
			RequestID: "req-fork-fail-timeout",
		},
	}
	if isSafeToRetry(forkFailReq) {
		t.Fatalf("expected ForkFromFailureRequest to not be safe to retry on timeout")
	}
}
