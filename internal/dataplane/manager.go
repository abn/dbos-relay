package dataplane

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
)

// ClientFactory creates a Client for an application's database configuration.
type ClientFactory func(cfg AppConfig) (Client, error)

// DefaultManager implements Manager.
type DefaultManager struct {
	mu          sync.RWMutex
	clients     map[pgtype.UUID]Client
	configs     map[pgtype.UUID]AppConfig
	initMu      map[pgtype.UUID]*sync.Mutex
	lastInitErr map[pgtype.UUID]time.Time
	factory     ClientFactory
	logger      *slog.Logger
}

// initBackoff is how long a failed data-plane client init suppresses
// further init attempts for the same application.
const initBackoff = 5 * time.Second

// NewManager creates a new DefaultManager with the provided client factory.
func NewManager(factory ClientFactory) *DefaultManager {
	if factory == nil {
		factory = NewSDKClient
	}
	return &DefaultManager{
		clients:     make(map[pgtype.UUID]Client),
		configs:     make(map[pgtype.UUID]AppConfig),
		initMu:      make(map[pgtype.UUID]*sync.Mutex),
		lastInitErr: make(map[pgtype.UUID]time.Time),
		factory:     factory,
		logger:      slog.Default(),
	}
}

// SetLogger configures the logger for data-plane management events.
func (m *DefaultManager) SetLogger(logger *slog.Logger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if logger != nil {
		m.logger = logger
	}
}

// RegisterApp configures and activates data-plane connectivity for an application.
// Note: The underlying DBOS Go SDK client requires system database schema version 123 or higher.
func (m *DefaultManager) RegisterApp(cfg AppConfig) error {
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("database URL is required")
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeRead
	}
	if cfg.Integration == "" {
		cfg.Integration = IntegrationHybrid
	}
	if !cfg.Integration.Valid() {
		return fmt.Errorf("invalid integration %q", cfg.Integration)
	}
	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = 5
	}
	if cfg.StatementTimeout <= 0 {
		cfg.StatementTimeout = 5 * time.Second
	}

	m.mu.Lock()

	// If a client was previously active for this app, close it first.
	if existing, ok := m.clients[cfg.ApplicationID]; ok {
		_ = existing.Close()
		delete(m.clients, cfg.ApplicationID)
	}

	m.configs[cfg.ApplicationID] = cfg
	appMu, ok := m.initMu[cfg.ApplicationID]
	if !ok {
		appMu = &sync.Mutex{}
		m.initMu[cfg.ApplicationID] = appMu
	}
	delete(m.lastInitErr, cfg.ApplicationID)
	m.mu.Unlock()

	// Attempt eager initialization under per-app mutex without holding the global manager lock
	appMu.Lock()
	defer appMu.Unlock()

	if client, err := m.factory(cfg); err == nil {
		m.mu.Lock()
		m.clients[cfg.ApplicationID] = client
		m.mu.Unlock()
	} else {
		m.mu.Lock()
		m.lastInitErr[cfg.ApplicationID] = time.Now()
		logger := m.logger
		m.mu.Unlock()
		if logger != nil {
			logger.Warn("failed eager data-plane client initialization",
				"applicationID", fmt.Sprintf("%x", cfg.ApplicationID.Bytes),
				"error", err,
			)
		}
	}

	return nil
}

// UnregisterApp shuts down and removes data-plane connectivity for an application.
func (m *DefaultManager) UnregisterApp(appID pgtype.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[appID]; ok {
		_ = client.Close()
		delete(m.clients, appID)
		delete(m.configs, appID)
		delete(m.initMu, appID)
		delete(m.lastInitErr, appID)
	}
}

// HasDataPlane returns true if data-plane access is configured for the application.
func (m *DefaultManager) HasDataPlane(appID pgtype.UUID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.configs[appID]
	return ok
}

// GetMode returns the configured access mode for the application.
func (m *DefaultManager) GetMode(appID pgtype.UUID) (Mode, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.configs[appID]
	if !ok {
		return "", false
	}
	return cfg.Mode, true
}

// GetIntegration returns the configured integration mode for the application.
func (m *DefaultManager) GetIntegration(appID pgtype.UUID) (Integration, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.configs[appID]
	if !ok {
		return "", false
	}
	if cfg.Integration == "" {
		return IntegrationHybrid, true
	}
	return cfg.Integration, true
}

func (m *DefaultManager) getOrInitClient(appID pgtype.UUID) (Client, AppConfig, error) {
	m.mu.RLock()
	cfg, ok := m.configs[appID]
	if !ok {
		m.mu.RUnlock()
		return nil, AppConfig{}, ErrNoDataPlaneConfigured
	}

	client, ok := m.clients[appID]
	appMu := m.initMu[appID]
	lastErr := m.lastInitErr[appID]
	m.mu.RUnlock()

	if ok && client != nil {
		return client, cfg, nil
	}
	if appMu == nil {
		return nil, cfg, fmt.Errorf("application not properly registered")
	}

	if time.Since(lastErr) < initBackoff {
		return nil, cfg, fmt.Errorf("failed to initialize data-plane client: backoff active")
	}

	appMu.Lock()
	defer appMu.Unlock()

	m.mu.RLock()
	client, ok = m.clients[appID]
	lastErr = m.lastInitErr[appID]
	m.mu.RUnlock()

	if ok && client != nil {
		return client, cfg, nil
	}
	if time.Since(lastErr) < initBackoff {
		return nil, cfg, fmt.Errorf("failed to initialize data-plane client: backoff active")
	}

	client, err := m.factory(cfg)
	if err != nil {
		m.mu.Lock()
		m.lastInitErr[appID] = time.Now()
		logger := m.logger
		m.mu.Unlock()
		if logger != nil {
			logger.Warn("failed lazy data-plane client initialization",
				"applicationID", fmt.Sprintf("%x", cfg.ApplicationID.Bytes),
				"error", err,
			)
		}
		return nil, cfg, fmt.Errorf("failed to initialize data-plane client: %w", err)
	}

	m.mu.Lock()
	m.clients[appID] = client
	delete(m.lastInitErr, appID)
	m.mu.Unlock()

	return client, cfg, nil
}

// Dispatch routes an operation through the application's data-plane connection.
func (m *DefaultManager) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	client, cfg, err := m.getOrInitClient(appID)
	if err != nil {
		return nil, err
	}

	// Verify permission if this is a mutating operation
	if isMutatingMessage(msg.GetMessageType()) && cfg.Mode != ModeReadWrite {
		return nil, ErrReadOnlyMode
	}

	// Apply statement timeout if configured
	dispatchCtx := ctx
	if cfg.StatementTimeout > 0 {
		var cancel context.CancelFunc
		dispatchCtx, cancel = context.WithTimeout(ctx, cfg.StatementTimeout)
		defer cancel()
	}

	return client.Dispatch(dispatchCtx, msg)
}

// GetEvent reads a single event value through the application's data plane. It
// is a read, so it works in read-only mode.
func (m *DefaultManager) GetEvent(ctx context.Context, appID pgtype.UUID, workflowID, key string) (WorkflowEvent, error) {
	client, cfg, err := m.getOrInitClient(appID)
	if err != nil {
		return WorkflowEvent{}, err
	}
	dispatchCtx := ctx
	if cfg.StatementTimeout > 0 {
		var cancel context.CancelFunc
		dispatchCtx, cancel = context.WithTimeout(ctx, cfg.StatementTimeout)
		defer cancel()
	}
	return client.GetEvent(dispatchCtx, workflowID, key)
}

// Enqueue writes a workflow row through the application's data plane. It is a
// mutation and therefore requires read-write mode.
func (m *DefaultManager) Enqueue(ctx context.Context, appID pgtype.UUID, input EnqueueWorkflowInput) (EnqueueWorkflowResult, error) {
	client, cfg, err := m.getOrInitClient(appID)
	if err != nil {
		return EnqueueWorkflowResult{}, err
	}
	if cfg.Mode != ModeReadWrite {
		return EnqueueWorkflowResult{}, ErrReadOnlyMode
	}
	dispatchCtx := ctx
	if cfg.StatementTimeout > 0 {
		var cancel context.CancelFunc
		dispatchCtx, cancel = context.WithTimeout(ctx, cfg.StatementTimeout)
		defer cancel()
	}
	return client.Enqueue(dispatchCtx, input)
}

// Send writes a message row through the application's data plane. It is a
// mutation and therefore requires read-write mode.
func (m *DefaultManager) Send(ctx context.Context, appID pgtype.UUID, input SendWorkflowMessageInput) error {
	client, cfg, err := m.getOrInitClient(appID)
	if err != nil {
		return err
	}
	if cfg.Mode != ModeReadWrite {
		return ErrReadOnlyMode
	}
	dispatchCtx := ctx
	if cfg.StatementTimeout > 0 {
		var cancel context.CancelFunc
		dispatchCtx, cancel = context.WithTimeout(ctx, cfg.StatementTimeout)
		defer cancel()
	}
	return client.Send(dispatchCtx, input)
}

// Recover re-enqueues the pending workflows of dead executors through the
// application's system database. It is a mutation and therefore requires the
// application to be configured in read-write mode.
func (m *DefaultManager) Recover(ctx context.Context, appID pgtype.UUID, executorIDs []string, appVersion string) ([]string, error) {
	client, cfg, err := m.getOrInitClient(appID)
	if err != nil {
		return nil, err
	}
	if cfg.Mode != ModeReadWrite {
		return nil, ErrReadOnlyMode
	}
	dispatchCtx := ctx
	if cfg.StatementTimeout > 0 {
		var cancel context.CancelFunc
		dispatchCtx, cancel = context.WithTimeout(ctx, cfg.StatementTimeout)
		defer cancel()
	}
	return client.Recover(dispatchCtx, executorIDs, appVersion)
}

// HasPendingWorkflows reports whether an executor still owns PENDING
// workflows. It is a read and works in read-only mode.
func (m *DefaultManager) HasPendingWorkflows(ctx context.Context, appID pgtype.UUID, executorID, appVersion string) (bool, error) {
	client, cfg, err := m.getOrInitClient(appID)
	if err != nil {
		return false, err
	}
	dispatchCtx := ctx
	if cfg.StatementTimeout > 0 {
		var cancel context.CancelFunc
		dispatchCtx, cancel = context.WithTimeout(ctx, cfg.StatementTimeout)
		defer cancel()
	}
	return client.HasPendingWorkflows(dispatchCtx, executorID, appVersion)
}

// Close gracefully terminates all active data-plane client pools.
func (m *DefaultManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for id, client := range m.clients {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(m.clients, id)
		delete(m.configs, id)
		delete(m.initMu, id)
		delete(m.lastInitErr, id)
	}
	return firstErr
}

// IsMutating reports whether a protocol message changes workflow or schedule
// state. Direct mode uses it to avoid retrying a non-idempotent mutation over
// the executor socket after a database failure.
func IsMutating(msgType protocol.MessageType) bool {
	return isMutatingMessage(msgType)
}

func isMutatingMessage(msgType protocol.MessageType) bool {
	switch msgType {
	case protocol.MessageTypeCancel,
		protocol.MessageTypeResume,
		protocol.MessageTypeForkWorkflow,
		protocol.MessageTypeRewindWorkflow,
		protocol.MessageTypeDelete,
		protocol.MessageTypeForkFromFailure,
		protocol.MessageTypeImportWorkflow,
		protocol.MessageTypeRetention,
		protocol.MessageTypePauseSchedule,
		protocol.MessageTypeResumeSchedule,
		protocol.MessageTypeBackfillSchedule,
		protocol.MessageTypeTriggerSchedule,
		protocol.MessageTypeSetLatestApplicationVersion:
		return true
	default:
		return false
	}
}
