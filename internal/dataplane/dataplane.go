package dataplane

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
)

var (
	ErrNoDataPlaneConfigured = errors.New("no data-plane configured for application")
	ErrReadOnlyMode          = errors.New("data-plane is in read-only mode")
	ErrUnsupportedOperation  = errors.New("operation not supported by data-plane")
)

// Mode defines the access mode for an application's data plane connection.
type Mode string

const (
	ModeRead      Mode = "read"
	ModeReadWrite Mode = "read-write"
)

// AppConfig specifies connection and guardrail parameters for an application's system database.
type AppConfig struct {
	ApplicationID    pgtype.UUID
	ApplicationName  string
	DatabaseURL      string
	Mode             Mode
	MaxConnections   int
	StatementTimeout time.Duration
}

// Client represents the data-plane interface to an application's system database.
type Client interface {
	Dispatch(ctx context.Context, msg protocol.Message) (protocol.Message, error)
	Close() error
}

// Manager manages per-application data-plane client pools and routing.
type Manager interface {
	RegisterApp(config AppConfig) error
	UnregisterApp(appID pgtype.UUID)
	HasDataPlane(appID pgtype.UUID) bool
	GetMode(appID pgtype.UUID) (Mode, bool)
	Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
	Close() error
}
