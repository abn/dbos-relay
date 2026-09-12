package hub

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

// RegistryDisconnector disconnects an executor in the database.
type RegistryDisconnector interface {
	DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error)
}

// Registry manages connected executors.
type Registry struct {
	mu     sync.RWMutex
	byApp  map[pgtype.UUID]map[string]*ExecutorConn
	rrApp  map[pgtype.UUID]uint64
	q      RegistryDisconnector
	logger *slog.Logger
}

// NewRegistry creates a new Registry.
func NewRegistry(q RegistryDisconnector, logger *slog.Logger) *Registry {
	return &Registry{
		byApp:  make(map[pgtype.UUID]map[string]*ExecutorConn),
		rrApp:  make(map[pgtype.UUID]uint64),
		q:      q,
		logger: logger,
	}
}

// Register adds a connection. Closes existing connection if one exists.
func (r *Registry) Register(conn *ExecutorConn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	appMap, ok := r.byApp[conn.appID]
	if !ok {
		appMap = make(map[string]*ExecutorConn)
		r.byApp[conn.appID] = appMap
	}

	if existing, ok := appMap[conn.executorID]; ok {
		// Do not block locking while closing, though Close takes multiplexer lock.
		// It's safer to close after we release registry lock, but we can call Close() here.
		// To avoid deadlock, we can do it in a goroutine.
		go func() { _ = existing.Close() }()
	}

	appMap[conn.executorID] = conn
}

// DrainAll removes and returns all active executor connections without closing them.
func (r *Registry) DrainAll() []*ExecutorConn {
	r.mu.Lock()
	defer r.mu.Unlock()

	var conns []*ExecutorConn
	for _, appMap := range r.byApp {
		for _, conn := range appMap {
			conns = append(conns, conn)
		}
	}
	r.byApp = make(map[pgtype.UUID]map[string]*ExecutorConn)
	r.rrApp = make(map[pgtype.UUID]uint64)
	return conns
}

// Unregister removes connection if it matches the currently registered instance and updates database.
// Returns true if the connection was actively registered and successfully unregistered.
func (r *Registry) Unregister(ctx context.Context, conn *ExecutorConn) bool {
	if conn == nil {
		return false
	}
	r.mu.Lock()
	appMap, ok := r.byApp[conn.appID]
	if !ok {
		r.mu.Unlock()
		return false
	}
	current, exists := appMap[conn.executorID]
	if !exists || current != conn {
		r.mu.Unlock()
		return false
	}

	delete(appMap, conn.executorID)
	if len(appMap) == 0 {
		delete(r.byApp, conn.appID)
		delete(r.rrApp, conn.appID)
	}
	r.mu.Unlock()

	if r.q != nil {
		disconnectCtx := ctx
		if disconnectCtx.Err() != nil {
			var cancel context.CancelFunc
			disconnectCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
		}
		if _, err := r.q.DisconnectExecutor(disconnectCtx, gen.DisconnectExecutorParams{
			ApplicationID: conn.appID,
			ExecutorID:    conn.executorID,
		}); err != nil {
			if r.logger != nil {
				r.logger.Error("failed to mark executor disconnected", "error", err, "executor_id", conn.executorID)
			}
		}
	}
	return true
}

// SelectExecutor returns an active executor connection for dispatch using round-robin selection.
func (r *Registry) SelectExecutor(appID pgtype.UUID) (*ExecutorConn, error) {
	return r.SelectExecutorWithExclusion(appID, "")
}

// SelectExecutorWithExclusion picks an active executor connection for dispatch using round-robin,
// excluding a specific executor ID if provided.
func (r *Registry) SelectExecutorWithExclusion(appID pgtype.UUID, excludeExecutorID string) (*ExecutorConn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	appMap, ok := r.byApp[appID]
	if !ok || len(appMap) == 0 {
		return nil, errors.New("no executors available for application")
	}

	var ids []string
	for id := range appMap {
		if id != excludeExecutorID {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		return nil, errors.New("no alternative executors available")
	}

	sort.Strings(ids)

	idx := r.rrApp[appID]
	r.rrApp[appID]++
	selectedID := ids[idx%uint64(len(ids))]

	return appMap[selectedID], nil
}

// ListConnected returns currently connected executors for the app.
func (r *Registry) ListConnected(appID pgtype.UUID) []*ExecutorConn {
	r.mu.RLock()
	defer r.mu.RUnlock()

	appMap, ok := r.byApp[appID]
	if !ok {
		return nil
	}

	conns := make([]*ExecutorConn, 0, len(appMap))
	for _, conn := range appMap {
		conns = append(conns, conn)
	}
	return conns
}

// GetExecutorConn returns the active connection for a specific executor ID.
func (r *Registry) GetExecutorConn(appID pgtype.UUID, executorID string) (*ExecutorConn, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	appMap, ok := r.byApp[appID]
	if !ok {
		return nil, errors.New("no executors available for application")
	}
	conn, ok := appMap[executorID]
	if !ok {
		return nil, errors.New("executor not connected")
	}
	return conn, nil
}
