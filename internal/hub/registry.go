package hub

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

// Registry manages connected executors.
type Registry struct {
	mu    sync.RWMutex
	byApp map[pgtype.UUID]map[string]*ExecutorConn
	q     *gen.Queries
}

// NewRegistry creates a new Registry.
func NewRegistry(q *gen.Queries) *Registry {
	return &Registry{
		byApp: make(map[pgtype.UUID]map[string]*ExecutorConn),
		q:     q,
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

// Unregister removes connection and updates database.
func (r *Registry) Unregister(ctx context.Context, appID pgtype.UUID, executorID string) {
	r.mu.Lock()
	if appMap, ok := r.byApp[appID]; ok {
		delete(appMap, executorID)
		if len(appMap) == 0 {
			delete(r.byApp, appID)
		}
	}
	r.mu.Unlock()

	if r.q != nil {
		_, _ = r.q.DisconnectExecutor(ctx, gen.DisconnectExecutorParams{
			ApplicationID: appID,
			ExecutorID:    executorID,
		})
	}
}

// SelectExecutor returns an active executor connection for dispatch.
func (r *Registry) SelectExecutor(appID pgtype.UUID) (*ExecutorConn, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	appMap, ok := r.byApp[appID]
	if !ok || len(appMap) == 0 {
		return nil, errors.New("no executors available for application")
	}

	// Pseudo-random selection (map iteration order is randomized in Go).
	for _, conn := range appMap {
		return conn, nil
	}

	return nil, errors.New("no executors available")
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
