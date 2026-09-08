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
	"github.com/abn/relay/internal/liveness"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

// LivenessTracker receives executor connection and disconnection events.
type LivenessTracker interface {
	OnConnect(ctx context.Context, appID pgtype.UUID, executorID, version string) error
	OnDisconnect(ctx context.Context, appID pgtype.UUID, executorID string)
}

// HubStore specifies the query methods required by the Hub.
type HubStore interface {
	AuthStore
	UpsertExecutor(ctx context.Context, arg gen.UpsertExecutorParams) (gen.Executor, error)
	DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error)
}

type Hub struct {
	store    HubStore
	registry *Registry
	config   *config.Config
	logger   *slog.Logger
	liveness LivenessTracker
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

func New(store any, cfg *config.Config, logger *slog.Logger) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	var hs HubStore
	if s, ok := store.(HubStore); ok {
		hs = s
	} else if sp, ok := store.(interface{ Queries() *gen.Queries }); ok && sp != nil {
		hs = sp.Queries()
	}
	return &Hub{
		store:    hs,
		registry: NewRegistry(hs),
		config:   cfg,
		logger:   logger,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// SetLivenessTracker sets the liveness manager to receive executor lifecycle events.
func (h *Hub) SetLivenessTracker(l LivenessTracker) {
	h.liveness = l
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

	appID, err := Authenticate(r.Context(), h.store, appName, conductorKey)
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
		md := make(map[string]any)
		if len(infoRes.ExecutorMetadata) > 0 {
			for k, v := range infoRes.ExecutorMetadata {
				md[k] = v
			}
		}
		if infoRes.Language != "" {
			md["language"] = infoRes.Language
		}
		if infoRes.DBOSVersion != "" {
			md["dbosVersion"] = infoRes.DBOSVersion
		}
		if len(md) > 0 {
			metadata, _ = json.Marshal(md)
		}
	} else if _, isReq := msg.(*protocol.ExecutorInfoRequest); isReq {
		_ = conn.Close(websocket.StatusPolicyViolation, "expected executor info response with details")
		return
	} else {
		_ = conn.Close(websocket.StatusPolicyViolation, "expected executor info message")
		return
	}

	// Persist executor
	_, err = h.store.UpsertExecutor(r.Context(), gen.UpsertExecutorParams{
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
		if h.liveness != nil {
			h.liveness.OnDisconnect(h.ctx, appID, executorID)
		}
	}

	execConn = NewExecutorConn(conn, appID, executorID, appName, appVersion, hostname, metadata, mux, unregister)

	h.registry.Register(execConn)
	if h.liveness != nil {
		_ = h.liveness.OnConnect(r.Context(), appID, executorID, appVersion)
	}

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

// FindHealthyPeers returns all currently connected peers for an application.
func (h *Hub) FindHealthyPeers(ctx context.Context, appID pgtype.UUID) ([]liveness.Peer, error) {
	conns := h.registry.ListConnected(appID)
	peers := make([]liveness.Peer, 0, len(conns))
	for _, c := range conns {
		peers = append(peers, liveness.Peer{
			AppID:              c.appID,
			ExecutorID:         c.executorID,
			ApplicationVersion: c.applicationVersion,
		})
	}
	return peers, nil
}

// SendRecovery sends a recovery request to a specific executor and awaits acknowledgment.
func (h *Hub) SendRecovery(ctx context.Context, appID pgtype.UUID, targetExecutorID string, req *protocol.RecoveryRequest) (*protocol.RecoveryResponse, error) {
	conn, err := h.registry.GetExecutorConn(appID, targetExecutorID)
	if err != nil {
		return nil, err
	}

	reqID := req.GetRequestID()
	if reqID == "" {
		return nil, errors.New("recovery request must have an ID")
	}

	ch, unregister := conn.mux.Register(reqID)
	defer unregister()

	if err := conn.WriteMessage(ctx, req); err != nil {
		return nil, fmt.Errorf("send recovery failed: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, h.config.ExecutorDeadline)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		return nil, errors.New("recovery request timed out")
	case res, ok := <-ch:
		if !ok {
			return nil, errors.New("connection closed while awaiting recovery response")
		}
		recRes, ok := res.(*protocol.RecoveryResponse)
		if !ok {
			return nil, fmt.Errorf("unexpected response message type: %v", res.GetMessageType())
		}
		return recRes, nil
	}
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
