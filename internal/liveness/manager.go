package liveness

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

const (
	DefaultExecutorTimeout = 60 * time.Second
)

// StoreQueries specifies the database operations required by the liveness manager.
type StoreQueries interface {
	GetApplicationByID(ctx context.Context, id pgtype.UUID) (gen.Application, error)
	DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error)
	SetExecutorDead(ctx context.Context, arg gen.SetExecutorDeadParams) (gen.Executor, error)
	DeleteExecutor(ctx context.Context, arg gen.DeleteExecutorParams) error
}

// RecoveryRunner triggers recovery dispatch for a dead executor.
type RecoveryRunner interface {
	RecoverDeadExecutor(ctx context.Context, appID pgtype.UUID, deadExecutorID, deadVersion string) error
}

type trackedExecutor struct {
	appID      pgtype.UUID
	executorID string
	version    string
	state      State
	timer      Timer
}

// Manager coordinates executor lifecycle states, grace period timers, and recovery.
type Manager struct {
	mu       sync.Mutex
	clock    Clock
	queries  StoreQueries
	recovery RecoveryRunner
	logger   *slog.Logger

	executors map[string]*trackedExecutor // Key: appID:executorID
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewManager creates a new liveness Manager.
func NewManager(clock Clock, q StoreQueries, recovery RecoveryRunner, logger *slog.Logger) *Manager {
	if clock == nil {
		clock = NewRealClock()
	}
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		clock:     clock,
		queries:   q,
		recovery:  recovery,
		logger:    logger,
		executors: make(map[string]*trackedExecutor),
		ctx:       ctx,
		cancel:    cancel,
	}
}

func executorKey(appID pgtype.UUID, executorID string) string {
	return fmt.Sprintf("%x:%s", appID.Bytes, executorID)
}

// OnConnect handles executor registration or reconnection.
func (m *Manager) OnConnect(ctx context.Context, appID pgtype.UUID, executorID, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := executorKey(appID, executorID)
	exec, exists := m.executors[key]
	if exists {
		if exec.state == StateDead {
			return ErrReconnectingDeadExecutor
		}
		if exec.timer != nil {
			exec.timer.Stop()
			exec.timer = nil
		}
	} else {
		exec = &trackedExecutor{
			appID:      appID,
			executorID: executorID,
		}
		m.executors[key] = exec
	}

	exec.version = version
	nextState, action, err := Transition(exec.state, EventConnect)
	if err != nil {
		return err
	}
	exec.state = nextState

	m.logger.Debug("executor connected",
		"executorID", executorID,
		"version", version,
		"action", action,
	)
	return nil
}

// SetRecovery configures the recovery runner for the manager.
func (m *Manager) SetRecovery(recovery RecoveryRunner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recovery = recovery
}

// DeleteExecutor removes an executor from the manager's tracked state and delegates to the store if configured.
func (m *Manager) DeleteExecutor(ctx context.Context, arg gen.DeleteExecutorParams) error {
	m.mu.Lock()
	key := executorKey(arg.ApplicationID, arg.ExecutorID)
	delete(m.executors, key)
	m.mu.Unlock()

	if m.queries != nil {
		return m.queries.DeleteExecutor(ctx, arg)
	}
	return nil
}

// EvictExecutor removes an executor from the manager's tracked in-memory state.
func (m *Manager) EvictExecutor(appID pgtype.UUID, executorID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := executorKey(appID, executorID)
	delete(m.executors, key)
}

// OnDisconnect handles executor disconnection, starting the grace period timer.
func (m *Manager) OnDisconnect(ctx context.Context, appID pgtype.UUID, executorID string) {
	timeout := m.resolveTimeout(ctx, appID)

	m.mu.Lock()
	defer m.mu.Unlock()

	key := executorKey(appID, executorID)
	exec, exists := m.executors[key]
	if !exists {
		exec = &trackedExecutor{
			appID:      appID,
			executorID: executorID,
			state:      StateConnected,
		}
		m.executors[key] = exec
	}

	nextState, action, err := Transition(exec.state, EventDisconnect)
	if err != nil {
		m.logger.Warn("invalid disconnect transition", "executorID", executorID, "error", err)
		return
	}
	exec.state = nextState

	if action == ActionStartGraceTimer {
		timer := m.clock.NewTimer(timeout)
		exec.timer = timer

		m.logger.Info("started executor disconnect grace period timer",
			"executorID", executorID,
			"timeout", timeout,
		)

		m.wg.Add(1)
		go m.watchGracePeriod(appID, executorID, exec.version, timer)
	}
}

func (m *Manager) resolveTimeout(ctx context.Context, appID pgtype.UUID) time.Duration {
	if m.queries == nil {
		return DefaultExecutorTimeout
	}

	app, err := m.queries.GetApplicationByID(ctx, appID)
	if err != nil || len(app.Settings) == 0 {
		return DefaultExecutorTimeout
	}

	var s struct {
		ExecutorTimeoutSecs int64 `json:"executorTimeoutSecs"`
	}
	if err := json.Unmarshal(app.Settings, &s); err != nil || s.ExecutorTimeoutSecs <= 0 {
		return DefaultExecutorTimeout
	}

	return time.Duration(s.ExecutorTimeoutSecs) * time.Second
}

func (m *Manager) watchGracePeriod(appID pgtype.UUID, executorID, version string, timer Timer) {
	defer m.wg.Done()

	select {
	case <-m.ctx.Done():
		return
	case <-timer.C():
	}

	m.mu.Lock()
	key := executorKey(appID, executorID)
	exec, exists := m.executors[key]
	if !exists || exec.timer != timer || exec.state != StateDisconnected {
		m.mu.Unlock()
		return
	}

	nextState, action, err := Transition(exec.state, EventGracePeriodExpired)
	if err != nil {
		m.mu.Unlock()
		m.logger.Error("transition on grace period expired failed", "error", err)
		return
	}
	exec.state = nextState
	exec.timer = nil
	m.mu.Unlock()

	m.logger.Warn("executor grace period expired, marked dead",
		"executorID", executorID,
		"action", action,
	)

	// Persist dead status to store
	if m.queries != nil {
		if _, err := m.queries.SetExecutorDead(m.ctx, gen.SetExecutorDeadParams{
			ApplicationID: appID,
			ExecutorID:    executorID,
		}); err != nil {
			m.logger.Error("failed to set executor dead in store", "executorID", executorID, "error", err)
		}
	}

	// Trigger recovery dispatch
	if action == ActionTriggerRecovery && m.recovery != nil {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			if err := m.recovery.RecoverDeadExecutor(m.ctx, appID, executorID, version); err != nil {
				m.logger.Error("recovery dispatch failed for dead executor",
					"executorID", executorID,
					"error", err,
				)
			}
		}()
	}
}

// Stop cancels all active timers and waits for ongoing background recovery tasks to complete.
func (m *Manager) Stop() {
	m.cancel()

	m.mu.Lock()
	for _, exec := range m.executors {
		if exec.timer != nil {
			exec.timer.Stop()
			exec.timer = nil
		}
	}
	m.mu.Unlock()

	m.wg.Wait()
}
