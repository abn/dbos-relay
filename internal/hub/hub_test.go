package hub

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/abn/relay/internal/config"
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
	ch, unregister := m.Register("req1")

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

type mockHubStore struct {
	memoryAuthStore
}

func newMockHubStore() *mockHubStore {
	return &mockHubStore{
		memoryAuthStore: *newMemoryAuthStore(),
	}
}

func (m *mockHubStore) UpsertExecutor(ctx context.Context, arg gen.UpsertExecutorParams) (gen.Executor, error) {
	return gen.Executor{
		ExecutorID: arg.ExecutorID,
	}, nil
}

func (m *mockHubStore) DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error) {
	return gen.Executor{}, nil
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

	// Wait past the handshake timeout
	time.Sleep(300 * time.Millisecond)

	// Assert all 25 sockets are closed by the server
	for i, c := range conns {
		readCtx, readCancel := context.WithTimeout(ctx, 500*time.Millisecond)
		closed := false
		for {
			_, _, err := c.Read(readCtx)
			if err != nil {
				closed = true
				break
			}
		}
		readCancel()
		if !closed {
			t.Errorf("expected client %d connection to be closed by server", i)
		}
	}

	// Assert runtime stack has no Hub.ServeHTTP frames lingering
	buf := make([]byte, 1024*1024)
	n := runtime.Stack(buf, true)
	stack := string(buf[:n])
	if strings.Contains(stack, "(*Hub).ServeHTTP") {
		t.Errorf("expected no lingering Hub.ServeHTTP goroutines, but found in stack: %s", stack)
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
