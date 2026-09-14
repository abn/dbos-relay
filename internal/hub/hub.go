package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

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

type LeaseStore interface {
	TouchExecutorLastSeen(ctx context.Context, arg gen.TouchExecutorLastSeenParams) error
}

type Hub struct {
	store            HubStore
	leaseStore       LeaseStore
	registry         *Registry
	config           *config.Config
	logger           *slog.Logger
	liveness         LivenessTracker
	instanceID       pgtype.UUID
	leaseDuration    time.Duration
	handshakeTimeout time.Duration
	readLimit        int64
	pingInterval     time.Duration
	pongTimeout      time.Duration
	wg               sync.WaitGroup
	ctx              context.Context
	cancel           context.CancelFunc
}

func New(store any, cfg *config.Config, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	var hs HubStore
	if s, ok := store.(HubStore); ok {
		hs = s
	} else if sp, ok := store.(interface{ Queries() *gen.Queries }); ok && sp != nil {
		hs = sp.Queries()
	}
	var ls LeaseStore
	if s, ok := store.(LeaseStore); ok {
		ls = s
	} else if sp, ok := store.(interface{ Queries() *gen.Queries }); ok && sp != nil {
		ls = sp.Queries()
	}
	return &Hub{
		store:            hs,
		leaseStore:       ls,
		registry:         NewRegistry(hs, logger),
		config:           cfg,
		logger:           logger,
		leaseDuration:    60 * time.Second,
		handshakeTimeout: 5 * time.Second,
		readLimit:        32 * 1024 * 1024,
		pingInterval:     20 * time.Second,
		pongTimeout:      25 * time.Second,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// SetInstanceID sets the local relay instance ID for executor ownership.
func (h *Hub) SetInstanceID(id pgtype.UUID) {
	h.instanceID = id
}

// SetLeaseDuration sets the executor lease duration (default 60s).
func (h *Hub) SetLeaseDuration(d time.Duration) {
	h.leaseDuration = d
}

// SetHandshakeTimeout sets the handshake timeout (useful in tests).
func (h *Hub) SetHandshakeTimeout(d time.Duration) {
	h.handshakeTimeout = d
}

// SetReadLimit sets the websocket message read limit (default 32 MiB).
func (h *Hub) SetReadLimit(limit int64) {
	h.readLimit = limit
}

// SetPingPongTimeouts sets the heartbeat ping interval and pong timeout (useful in tests).
func (h *Hub) SetPingPongTimeouts(interval, timeout time.Duration) {
	h.pingInterval = interval
	h.pongTimeout = timeout
}

// SetLivenessTracker sets the liveness manager to receive executor lifecycle events.
func (h *Hub) SetLivenessTracker(l LivenessTracker) {
	h.liveness = l
}

func isValidIdentifier(s string, allowEmpty bool) bool {
	if s == "" {
		return allowEmpty
	}
	if len(s) > 255 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/websocket/") {
		http.NotFound(w, r)
		return
	}
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

	appID, err := Authenticate(r.Context(), h.store, appName, conductorKey, h.config.AuthEnabled())
	if err != nil {
		HandleAuthError(w, r, err)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.logger.Error("websocket accept failed", "error", err)
		return
	}
	limit := h.readLimit
	if limit <= 0 {
		limit = 32 * 1024 * 1024
	}
	conn.SetReadLimit(limit)

	timeout := h.handshakeTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	handshakeCtx, cancelHandshake := context.WithTimeout(r.Context(), timeout)
	defer cancelHandshake()

	registered := false
	defer func() {
		if !registered {
			_ = conn.Close(websocket.StatusPolicyViolation, "handshake failed or timed out")
		}
	}()

	stopHandshakeWatcher := make(chan struct{})
	defer close(stopHandshakeWatcher)
	go func() {
		select {
		case <-h.ctx.Done():
			if !registered {
				_ = conn.Close(websocket.StatusPolicyViolation, "hub closed")
			}
		case <-handshakeCtx.Done():
			if !registered {
				_ = conn.Close(websocket.StatusPolicyViolation, "handshake timed out")
			}
		case <-stopHandshakeWatcher:
		}
	}()

	// Send executor_info request to prompt executor registration per D7 protocol
	infoReq := &protocol.ExecutorInfoRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: "req-init-info",
		},
	}
	if reqData, err := protocol.Encode(infoReq); err == nil {
		if err := conn.Write(handshakeCtx, websocket.MessageText, reqData); err != nil {
			h.logger.Error("failed to write executor_info prompt", "error", err)
			return
		}
	}

	// First message must be executor info
	typ, data, err := conn.Read(context.WithoutCancel(handshakeCtx))
	if err != nil {
		h.logger.Error("failed to read executor info", "error", err)
		return
	}
	if typ != websocket.MessageText {
		_ = conn.Close(websocket.StatusUnsupportedData, "expected text message")
		return
	}

	msg, err := protocol.DecodeResponse(data)
	if err != nil {
		_ = conn.Close(websocket.StatusUnsupportedData, "invalid protocol message")
		return
	}

	var executorID, appVersion, hostname string
	var metadata []byte

	if infoRes, isRes := msg.(*protocol.ExecutorInfoResponse); isRes {
		if infoRes.ErrorMessage != nil {
			h.logger.Error("executor reported error during handshake", "app_name", appName, "error", *infoRes.ErrorMessage)
			_ = conn.Close(websocket.StatusPolicyViolation, "executor reported error during handshake")
			return
		}
		if !isValidIdentifier(infoRes.ExecutorID, false) {
			h.logger.Warn("executor connected with invalid executor_id", "app_name", appName)
			_ = conn.Close(websocket.StatusPolicyViolation, "invalid executor id")
			return
		}
		executorID = infoRes.ExecutorID
		appVersion = infoRes.ApplicationVersion
		if infoRes.Hostname != nil {
			if !isValidIdentifier(*infoRes.Hostname, true) {
				h.logger.Warn("executor connected with invalid hostname", "app_name", appName)
				_ = conn.Close(websocket.StatusPolicyViolation, "invalid hostname")
				return
			}
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

	var leaseExpires pgtype.Timestamptz
	if h.instanceID.Valid {
		leaseExpires = pgtype.Timestamptz{
			Time:  time.Now().Add(h.leaseDuration),
			Valid: true,
		}
	}

	// Persist executor
	_, err = h.store.UpsertExecutor(r.Context(), gen.UpsertExecutorParams{
		ApplicationID:      appID,
		ExecutorID:         executorID,
		ApplicationVersion: appVersion,
		Hostname:           hostname,
		Metadata:           metadata,
		OwnerInstanceID:    h.instanceID,
		LeaseExpiresAt:     leaseExpires,
	})
	if err != nil {
		h.logger.Error("failed to persist executor", "error", err)
		_ = conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	mux := NewMultiplexer()

	var execConn *ExecutorConn
	unregister := func() {
		if execConn != nil && h.registry.Unregister(h.ctx, execConn) {
			if h.liveness != nil {
				h.liveness.OnDisconnect(h.ctx, appID, executorID)
			}
		}
	}

	execConn = NewExecutorConn(conn, appID, executorID, appName, appVersion, hostname, metadata, mux, unregister)
	if h.pingInterval > 0 && h.pongTimeout > 0 {
		execConn.SetPingPongTimeouts(h.pingInterval, h.pongTimeout)
	}
	if h.instanceID.Valid && h.leaseStore != nil {
		execConn.SetTouchLease(func(ctx context.Context) error {
			renewalExpires := pgtype.Timestamptz{
				Time:  time.Now().Add(h.leaseDuration),
				Valid: true,
			}
			return h.leaseStore.TouchExecutorLastSeen(ctx, gen.TouchExecutorLastSeenParams{
				ApplicationID:  appID,
				ExecutorID:     executorID,
				LeaseExpiresAt: renewalExpires,
			})
		})
	}

	h.registry.Register(execConn)
	registered = true
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

	ch, unregister, err := conn.mux.Register(reqID)
	if err != nil {
		return nil, err
	}
	defer unregister()

	timeoutCtx, cancel := context.WithTimeout(ctx, h.config.ExecutorDeadline)
	defer cancel()

	if err := conn.WriteMessage(timeoutCtx, req); err != nil {
		return nil, fmt.Errorf("send recovery failed: %w", err)
	}

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

// isSafeToRetry returns true if the message is idempotent or a read-only query
// that is safe to retry on another executor upon timeout or disconnect.
func isSafeToRetry(req protocol.Message) bool {
	if req == nil {
		return false
	}
	switch req.GetMessageType() {
	case protocol.MessageTypeExecutorInfo,
		protocol.MessageTypeListWorkflows,
		protocol.MessageTypeListQueuedWorkflows,
		protocol.MessageTypeListSteps,
		protocol.MessageTypeGetWorkflow,
		protocol.MessageTypeExistPendingWorkflows,
		protocol.MessageTypeGetMetrics,
		protocol.MessageTypeListSchedules,
		protocol.MessageTypeGetSchedule,
		protocol.MessageTypeGetWorkflowEvents,
		protocol.MessageTypeGetWorkflowNotifications,
		protocol.MessageTypeGetWorkflowStreams,
		protocol.MessageTypeGetWorkflowAggregates,
		protocol.MessageTypeGetStepAggregates,
		protocol.MessageTypeListApplicationVersions,
		protocol.MessageTypeListQueues,
		protocol.MessageTypeGetQueue,
		protocol.MessageTypeCancel,
		protocol.MessageTypeResume:
		return true
	default:
		return false
	}
}

func (h *Hub) dispatchToConn(ctx context.Context, conn *ExecutorConn, reqID string, req protocol.Message) (protocol.Message, error, error) {
	ch, unregister, err := conn.mux.Register(reqID)
	if err != nil {
		return nil, nil, err
	}
	defer unregister()

	timeoutCtx, cancel := context.WithTimeout(ctx, h.config.ExecutorDeadline)
	defer cancel()

	if err := conn.WriteMessage(timeoutCtx, req); err != nil {
		return nil, err, fmt.Errorf("dispatch failed: %w", err)
	}

	select {
	case <-timeoutCtx.Done():
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("request timed out")
	case res, ok := <-ch:
		if !ok {
			return nil, nil, errors.New("connection closed while awaiting response")
		}
		return res, nil, nil
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

	res, writeErr, dispatchErr := h.dispatchToConn(ctx, conn, reqID, req)
	if dispatchErr == nil {
		return res, nil
	}

	// Retry on an alternative healthy executor if write failed or if request is safe to retry on timeout
	if writeErr != nil || isSafeToRetry(req) {
		altConn, altErr := h.registry.SelectExecutorWithExclusion(appID, conn.executorID)
		if altErr == nil {
			altRes, _, altDispatchErr := h.dispatchToConn(ctx, altConn, reqID, req)
			if altDispatchErr == nil {
				return altRes, nil
			}
			return nil, altDispatchErr
		}
	}

	return nil, dispatchErr
}

func (h *Hub) Close() error {
	// Safely close active connections so peers receive a clean WebSocket close frame.
	conns := h.registry.DrainAll()
	var wg sync.WaitGroup
	for _, conn := range conns {
		wg.Add(1)
		go func(c *ExecutorConn) {
			defer wg.Done()
			_ = c.Close()
		}(conn)
	}
	wg.Wait()

	h.cancel()
	h.wg.Wait()
	return nil
}
