package liveness

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/safego"
	"github.com/abn/relay/internal/store/gen"
)

const (
	DefaultExecutorTimeout = 60 * time.Second
)

// StoreQueries specifies the database operations required by the liveness manager.
type StoreQueries interface {
	GetApplicationByID(ctx context.Context, id pgtype.UUID) (gen.Application, error)
	GetExecutorByID(ctx context.Context, arg gen.GetExecutorByIDParams) (gen.Executor, error)
	DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error)
	SetExecutorDead(ctx context.Context, arg gen.SetExecutorDeadParams) (gen.Executor, error)
	DeleteExecutor(ctx context.Context, arg gen.DeleteExecutorParams) error
	ListDeadExecutorsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]gen.Executor, error)
}

// RecoveryRunner triggers recovery dispatch for a dead executor.
type RecoveryRunner interface {
	RecoverDeadExecutor(ctx context.Context, appID pgtype.UUID, deadExecutorID, deadVersion string) error
}

type trackedExecutor struct {
	appID       pgtype.UUID
	executorID  string
	version     string
	state       State
	timer       Timer
	cancelTimer chan struct{}
}

// Manager coordinates executor lifecycle states, grace period timers, and recovery.
type Manager struct {
	mu         sync.Mutex
	clock      Clock
	queries    StoreQueries
	recovery   RecoveryRunner
	logger     *slog.Logger
	instanceID pgtype.UUID

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

// SetInstanceID sets the local Relay instance identity for cluster ownership checks.
func (m *Manager) SetInstanceID(id pgtype.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instanceID = id
}

// TrackedCount returns the number of executors currently tracked by the manager.
func (m *Manager) TrackedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.executors)
}

// OnConnect handles executor registration or reconnection.
func (m *Manager) OnConnect(ctx context.Context, appID pgtype.UUID, executorID, version string) error {
	m.mu.Lock()

	key := executorKey(appID, executorID)
	exec, exists := m.executors[key]
	if exists {
		if exec.state == StateDead {
			m.mu.Unlock()
			return ErrReconnectingDeadExecutor
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
		m.mu.Unlock()
		return err
	}
	exec.state = nextState
	if action == ActionCancelGraceTimer {
		if exec.timer != nil {
			exec.timer.Stop()
			exec.timer = nil
		}
		if exec.cancelTimer != nil {
			close(exec.cancelTimer)
			exec.cancelTimer = nil
		}
	}
	m.mu.Unlock()

	m.logger.Debug("executor connected",
		"executorID", executorID,
		"version", version,
		"action", action,
	)

	// Sweep any dead executors for this application now that a healthy peer is available.
	if m.recovery != nil && m.queries != nil {
		//nolint:contextcheck // background sweep runs independently of the registration handshake
		safego.Go(m.logger, "liveness-sweep-dead", func() {
			m.SweepDeadExecutors(m.ctx, appID)
		})
	}

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
	if e, ok := m.executors[key]; ok {
		s, _, _ := Transition(e.state, EventDelete)
		e.state = s
	}
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

// AdoptDisconnected registers an adopted orphaned executor in StateDisconnected and arms a grace timer.
func (m *Manager) AdoptDisconnected(appID pgtype.UUID, executorID, version string) {
	timeout := m.resolveTimeout(m.ctx, appID)

	m.mu.Lock()
	defer m.mu.Unlock()

	key := executorKey(appID, executorID)
	exec, exists := m.executors[key]
	if !exists {
		exec = &trackedExecutor{
			appID:      appID,
			executorID: executorID,
			version:    version,
			state:      StateDisconnected,
		}
		m.executors[key] = exec
	} else {
		exec.version = version
		exec.state = StateDisconnected
		if exec.timer != nil {
			exec.timer.Stop()
			exec.timer = nil
		}
		if exec.cancelTimer != nil {
			close(exec.cancelTimer)
			exec.cancelTimer = nil
		}
	}

	timer := m.clock.NewTimer(timeout)
	exec.timer = timer
	timerDone := make(chan struct{})
	exec.cancelTimer = timerDone

	m.logger.Info("adopted orphaned executor, armed grace timer",
		"executorID", executorID,
		"timeout", timeout,
	)

	m.wg.Add(1)
	safego.Go(m.logger, "liveness-watch-grace-period", func() {
		m.watchGracePeriod(appID, executorID, version, timer, timerDone)
	})
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
		timerDone := make(chan struct{})
		exec.cancelTimer = timerDone

		m.logger.Info("started executor disconnect grace period timer",
			"executorID", executorID,
			"timeout", timeout,
		)

		version := exec.version
		m.wg.Add(1)
		//nolint:contextcheck // grace period timer outlives the disconnect notification context
		safego.Go(m.logger, "liveness-watch-grace-period", func() {
			m.watchGracePeriod(appID, executorID, version, timer, timerDone)
		})
	}
}

func (m *Manager) resolveTimeout(ctx context.Context, appID pgtype.UUID) time.Duration {
	if m.queries == nil {
		return DefaultExecutorTimeout
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	app, err := m.queries.GetApplicationByID(lookupCtx, appID)
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

func (m *Manager) watchGracePeriod(appID pgtype.UUID, executorID, version string, timer Timer, timerDone chan struct{}) {
	defer m.wg.Done()

	select {
	case <-m.ctx.Done():
		return
	case <-timerDone:
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
	exec.cancelTimer = nil
	m.mu.Unlock()

	// Check if the executor reconnected to another instance before marking dead.
	if m.queries != nil {
		lookupCtx, cancel := context.WithTimeout(m.ctx, 2*time.Second)
		execRow, err := m.queries.GetExecutorByID(lookupCtx, gen.GetExecutorByIDParams{
			ApplicationID: appID,
			ExecutorID:    executorID,
		})
		cancel()
		if err == nil && execRow.Status == "connected" {
			isOtherOwner := m.instanceID.Valid && execRow.OwnerInstanceID.Valid && execRow.OwnerInstanceID != m.instanceID
			isFutureLease := execRow.LeaseExpiresAt.Valid && execRow.LeaseExpiresAt.Time.After(time.Now())
			if isOtherOwner && isFutureLease {
				m.logger.Info("executor reconnected to another instance, cancelling dead transition",
					"executorID", executorID,
					"owner", execRow.OwnerInstanceID,
				)
				m.mu.Lock()
				delete(m.executors, key)
				m.mu.Unlock()
				return
			}
		}
	}

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
		safego.Go(m.logger, "liveness-recovery-dispatch", func() {
			defer m.wg.Done()
			if err := m.recovery.RecoverDeadExecutor(m.ctx, appID, executorID, version); err != nil {
				m.logger.Error("recovery dispatch failed for dead executor",
					"executorID", executorID,
					"error", err,
				)
				m.mu.Lock()
				if e, ok := m.executors[key]; ok {
					s, _, terr := Transition(e.state, EventRecoveryFailed)
					if terr == nil {
						e.state = s
					}
				}
				m.mu.Unlock()
			}
		})
	}
}

// SweepDeadExecutors checks for dead executors of an application and attempts recovery.
func (m *Manager) SweepDeadExecutors(ctx context.Context, appID pgtype.UUID) {
	if m.queries == nil || m.recovery == nil {
		return
	}

	sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	deadExecs, err := m.queries.ListDeadExecutorsByApplication(sweepCtx, appID)
	if err != nil || len(deadExecs) == 0 {
		return
	}

	for _, exec := range deadExecs {
		if err := m.recovery.RecoverDeadExecutor(sweepCtx, appID, exec.ExecutorID, exec.ApplicationVersion); err != nil {
			m.logger.Debug("sweep recovery dispatch not completed",
				"executorID", exec.ExecutorID,
				"error", err,
			)
		}
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
		if exec.cancelTimer != nil {
			close(exec.cancelTimer)
			exec.cancelTimer = nil
		}
	}
	m.mu.Unlock()

	m.wg.Wait()
}
