package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
)

// Provenance: Server-initiated liveness probe. Mirrors the executor's own 20s ping cadence (see docs/discovery/recovery-params.md); the protocol-facing server value is executorPingWait = 25s.
const (
	pingInterval     = 20 * time.Second
	executorPingWait = 25 * time.Second
)

type HubCallback func(conn *ExecutorConn, msg protocol.Message)

// ExecutorConn wraps a websocket connection to an executor.
type ExecutorConn struct {
	appID              pgtype.UUID
	executorID         string
	appName            string
	applicationVersion string
	hostname           string
	metadata           []byte

	conn *websocket.Conn
	mux  *Multiplexer

	unregister func()
	closeOnce  sync.Once
	closed     chan struct{}

	pingInterval time.Duration
	pongTimeout  time.Duration

	touchLease func(ctx context.Context) error
}

// NewExecutorConn creates a new executor connection.
func NewExecutorConn(
	conn *websocket.Conn,
	appID pgtype.UUID,
	executorID, appName, appVersion, hostname string,
	metadata []byte,
	mux *Multiplexer,
	unregister func(),
) *ExecutorConn {
	return &ExecutorConn{
		appID:              appID,
		executorID:         executorID,
		appName:            appName,
		applicationVersion: appVersion,
		hostname:           hostname,
		metadata:           metadata,
		conn:               conn,
		mux:                mux,
		unregister:         unregister,
		closed:             make(chan struct{}),
		pingInterval:       pingInterval,
		pongTimeout:        executorPingWait,
	}
}

// SetPingPongTimeouts configures heartbeat intervals (useful in tests).
func (c *ExecutorConn) SetPingPongTimeouts(interval, timeout time.Duration) {
	c.pingInterval = interval
	c.pongTimeout = timeout
}

// SetTouchLease sets the lease renewal function called on each heartbeat ping.
func (c *ExecutorConn) SetTouchLease(fn func(ctx context.Context) error) {
	c.touchLease = fn
}

// WriteMessage encodes and writes a protocol message to the connection safely.
func (c *ExecutorConn) WriteMessage(ctx context.Context, msg protocol.Message) error {
	data, err := protocol.Encode(msg)
	if err != nil {
		return fmt.Errorf("encode failed: %w", err)
	}

	return c.conn.Write(ctx, websocket.MessageText, data)
}

// ReadPump runs the read loop for the connection.
func (c *ExecutorConn) ReadPump(ctx context.Context, onMessage HubCallback) {
	defer func() { _ = c.Close() }()

	stopWatcher := make(chan struct{})
	defer close(stopWatcher)
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-c.closed:
		case <-stopWatcher:
		}
	}()

	for {
		typ, data, err := c.conn.Read(context.WithoutCancel(ctx))
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}

		var msg protocol.Message
		reqID := peekRequestID(data)
		if reqID != "" && c.mux.IsPending(reqID) {
			msg, err = protocol.DecodeResponse(data)
		} else {
			msg, err = protocol.Decode(data)
		}
		if err != nil {
			continue // Or log it
		}

		// Route response to multiplexer if it has a request ID and is a response.
		// For simplicity, we can let multiplexer decide. If it's not pending, we pass to onMessage.
		if msg.GetRequestID() != "" {
			// Some messages are requests from executor. Multiplexer RouteResponse will return false
			// if it's not a known pending request ID.
			if c.mux.RouteResponse(msg) {
				continue
			}
		}

		if onMessage != nil {
			onMessage(c, msg)
		}
	}
}

func peekRequestID(data []byte) string {
	var raw struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(data, &raw)
	return raw.RequestID
}

// HeartbeatPump runs the ping/pong loop for the connection.
func (c *ExecutorConn) HeartbeatPump(ctx context.Context) {
	interval := c.pingInterval
	if interval <= 0 {
		interval = pingInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer func() { _ = c.Close() }()

	timeout := c.pongTimeout
	if timeout <= 0 {
		timeout = executorPingWait
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case <-ticker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, timeout)
			err := c.conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				return
			}
			if c.touchLease != nil {
				touchCtx, touchCancel := context.WithTimeout(ctx, timeout)
				err := c.touchLease(touchCtx)
				touchCancel()
				if err != nil {
					return
				}
			}
		}
	}
}

// Close gracefully closes the connection.
func (c *ExecutorConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		c.mux.CancelAll(fmt.Errorf("connection closed"))
		if c.unregister != nil {
			c.unregister()
		}
		err = c.conn.Close(websocket.StatusNormalClosure, "disconnecting")
	})
	return err
}
