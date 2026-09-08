package dataplane

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
)

// ClientFactory creates a Client for an application's database configuration.
type ClientFactory func(cfg AppConfig) (Client, error)

// DefaultManager implements Manager.
type DefaultManager struct {
	mu      sync.RWMutex
	clients map[pgtype.UUID]Client
	configs map[pgtype.UUID]AppConfig
	factory ClientFactory
}

// NewManager creates a new DefaultManager with the provided client factory.
func NewManager(factory ClientFactory) *DefaultManager {
	if factory == nil {
		factory = NewSDKClient
	}
	return &DefaultManager{
		clients: make(map[pgtype.UUID]Client),
		configs: make(map[pgtype.UUID]AppConfig),
		factory: factory,
	}
}

// RegisterApp configures and activates data-plane connectivity for an application.
func (m *DefaultManager) RegisterApp(cfg AppConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg.DatabaseURL == "" {
		return fmt.Errorf("database URL is required")
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeRead
	}
	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = 5
	}
	if cfg.StatementTimeout <= 0 {
		cfg.StatementTimeout = 5 * time.Second
	}

	// If a client was previously active for this app, close it first.
	if existing, ok := m.clients[cfg.ApplicationID]; ok {
		_ = existing.Close()
	}

	client, err := m.factory(cfg)
	if err != nil {
		return fmt.Errorf("failed to create data-plane client: %w", err)
	}

	m.clients[cfg.ApplicationID] = client
	m.configs[cfg.ApplicationID] = cfg
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
	}
}

// HasDataPlane returns true if data-plane access is configured for the application.
func (m *DefaultManager) HasDataPlane(appID pgtype.UUID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.clients[appID]
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

// Dispatch routes an operation through the application's data-plane connection.
func (m *DefaultManager) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	m.mu.RLock()
	client, ok := m.clients[appID]
	cfg := m.configs[appID]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrNoDataPlaneConfigured
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
	}
	return firstErr
}

func isMutatingMessage(msgType protocol.MessageType) bool {
	switch msgType {
	case protocol.MessageTypeCancel,
		protocol.MessageTypeResume,
		protocol.MessageTypeForkWorkflow,
		protocol.MessageTypeDelete:
		return true
	default:
		return false
	}
}
