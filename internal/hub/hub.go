package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
)

type Hub struct {
	store    *store.Store
	registry *Registry
	config   *config.Config
	logger   *slog.Logger
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func New(store *store.Store, cfg *config.Config, logger *slog.Logger) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		store:    store,
		registry: NewRegistry(store.Queries()),
		config:   cfg,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Simple path routing /websocket/{appName}/{conductorKey}
	pathParts := r.URL.Path[len("/websocket/"):]
	appName := ""
	conductorKey := ""
	for i, c := range pathParts {
		if c == '/' {
			appName = pathParts[:i]
			conductorKey = pathParts[i+1:]
			break
		}
	}

	appID, err := Authenticate(r.Context(), h.store.Queries(), appName, conductorKey)
	if err != nil {
		HandleAuthError(w, r, err)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.logger.Error("websocket accept failed", "error", err)
		return
	}

	// First message must be executor info
	typ, data, err := conn.Read(r.Context())
	if err != nil {
		h.logger.Error("failed to read executor info", "error", err)
		_ = conn.Close(websocket.StatusPolicyViolation, "missing executor info")
		return
	}
	if typ != websocket.MessageText {
		_ = conn.Close(websocket.StatusUnsupportedData, "expected text message")
		return
	}

	msg, err := protocol.Decode(data)
	if err != nil {
		_ = conn.Close(websocket.StatusUnsupportedData, "invalid protocol message")
		return
	}

	var executorID, appVersion, hostname string
	var metadata []byte

	if infoRes, isRes := msg.(*protocol.ExecutorInfoResponse); isRes {
		executorID = infoRes.ExecutorID
		appVersion = infoRes.ApplicationVersion
		if infoRes.Hostname != nil {
			hostname = *infoRes.Hostname
		}
		if len(infoRes.ExecutorMetadata) > 0 {
			metadata, _ = json.Marshal(infoRes.ExecutorMetadata)
		}
	} else if _, isReq := msg.(*protocol.ExecutorInfoRequest); isReq {
		_ = conn.Close(websocket.StatusPolicyViolation, "expected executor info response with details")
		return
	} else {
		_ = conn.Close(websocket.StatusPolicyViolation, "expected executor info message")
		return
	}

	// Persist executor
	_, err = h.store.Queries().UpsertExecutor(r.Context(), gen.UpsertExecutorParams{
		ApplicationID:      appID,
		ExecutorID:         executorID,
		ApplicationVersion: appVersion,
		Hostname:           hostname,
		Metadata:           metadata,
	})
	if err != nil {
		h.logger.Error("failed to persist executor", "error", err)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	mux := NewMultiplexer()

	var execConn *ExecutorConn
	unregister := func() {
		h.registry.Unregister(h.ctx, appID, executorID)
	}

	execConn = NewExecutorConn(conn, appID, executorID, appName, appVersion, hostname, metadata, mux, unregister)

	h.registry.Register(execConn)

	h.wg.Add(2)
	go func() {
		defer h.wg.Done()
		execConn.ReadPump(h.ctx, func(c *ExecutorConn, m protocol.Message) {
			// Incoming messages from executor not handled by multiplexer
			h.logger.Debug("received message from executor", "executorID", executorID, "type", m.GetMessageType())
		})
	}()
	go func() {
		defer h.wg.Done()
		execConn.HeartbeatPump(h.ctx)
	}()
}

// Dispatch sends a request to an executor and waits for the response.
func (h *Hub) Dispatch(ctx context.Context, appID pgtype.UUID, req protocol.Message) (protocol.Message, error) {
	conn, err := h.registry.SelectExecutor(appID)
	if err != nil {
		return nil, err
	}

	reqID := req.GetRequestID()
	if reqID == "" {
		return nil, errors.New("request must have an ID")
	}

	ch, unregister := conn.mux.Register(reqID)
	defer unregister()

	if err := conn.WriteMessage(ctx, req); err != nil {
		return nil, fmt.Errorf("dispatch failed: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, h.config.ExecutorDeadline)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		return nil, errors.New("request timed out")
	case res, ok := <-ch:
		if !ok {
			return nil, errors.New("connection closed while awaiting response")
		}
		return res, nil
	}
}

func (h *Hub) Close() error {
	h.cancel()
	h.wg.Wait()
	// Registry close
	h.registry.mu.Lock()
	defer h.registry.mu.Unlock()
	for _, appMap := range h.registry.byApp {
		for _, conn := range appMap {
			_ = conn.Close()
		}
	}
	return nil
}
