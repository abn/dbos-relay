package hub

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
)

// Provenance: Server-initiated liveness probe. Mirrors the executor's own 20s ping cadence (see docs/discovery/recovery-params.md); the protocol-facing server value is executorPingWait = 25s.
const (
	pingInterval = 20 * time.Second
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
	mu   sync.Mutex // Protects WriteMessage
	mux  *Multiplexer

	unregister func()
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
	}
}

// WriteMessage encodes and writes a protocol message to the connection safely.
func (c *ExecutorConn) WriteMessage(ctx context.Context, msg protocol.Message) error {
	data, err := protocol.Encode(msg)
	if err != nil {
		return fmt.Errorf("encode failed: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.conn.Write(ctx, websocket.MessageText, data)
}

// ReadPump runs the read loop for the connection.
func (c *ExecutorConn) ReadPump(ctx context.Context, onMessage HubCallback) {
	defer func() { _ = c.Close() }()

	for {
		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}

		msg, err := protocol.Decode(data)
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

// HeartbeatPump runs the ping/pong loop for the connection.
func (c *ExecutorConn) HeartbeatPump(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	defer func() { _ = c.Close() }()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.mu.Lock()
			err := c.conn.Ping(ctx)
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

// Close gracefully closes the connection.
func (c *ExecutorConn) Close() error {
	c.mux.CancelAll(fmt.Errorf("connection closed"))
	if c.unregister != nil {
		c.unregister()
	}
	return c.conn.Close(websocket.StatusNormalClosure, "disconnecting")
}
