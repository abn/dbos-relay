package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// Integration selects how Relay reaches an application.
type Integration string

const (
	// IntegrationHybrid prefers a live executor and falls back to the system
	// database when no executor is reachable. This is the default for existing
	// data_plane configurations.
	IntegrationHybrid Integration = "hybrid"
	// IntegrationDirect prefers the system database for every operation and uses
	// a live executor only when the database path cannot serve the operation.
	IntegrationDirect Integration = "direct"
	// IntegrationAttached uses the executor WebSocket only and ignores the
	// database configuration.
	IntegrationAttached Integration = "attached"
)

// Valid reports whether the integration value is known.
func (i Integration) Valid() bool {
	switch i {
	case IntegrationHybrid, IntegrationDirect, IntegrationAttached:
		return true
	default:
		return false
	}
}

// AppConfig specifies connection and guardrail parameters for an application's system database.
// Relay v1 actively enforces bounded connection pool sizing (MaxConnections) and per-query
// cancellation and statement timeouts (StatementTimeout). Additional scale-tier guardrails,
// including rate limits, read replica pool routing, and dedicated data-plane OpenMetrics
// counters and latency histograms, are planned for a subsequent release.
type AppConfig struct {
	ApplicationID    pgtype.UUID
	ApplicationName  string
	DatabaseURL      string
	Mode             Mode
	Integration      Integration
	MaxConnections   int
	StatementTimeout time.Duration
}

// EnqueueWorkflowInput describes a workflow to enqueue through the application
// system database. Payload is a JSON value and is stored with the DBOS portable
// JSON serializer, wrapped as the workflow's single positional argument.
type EnqueueWorkflowInput struct {
	WorkflowName       string
	QueueName          string
	Payload            json.RawMessage
	WorkflowID         *string
	ApplicationVersion *string
	QueuePartitionKey  *string
}

// EnqueueWorkflowResult reports the workflow ID created by an enqueue.
type EnqueueWorkflowResult struct {
	WorkflowID string
}

// SendWorkflowMessageInput describes a message to deliver to a workflow. Payload
// is a JSON value stored with the DBOS portable JSON serializer.
type SendWorkflowMessageInput struct {
	DestinationID  string
	Topic          string
	Payload        json.RawMessage
	IdempotencyKey *string
}

// Validate checks that the input can be written without re-encoding an opaque
// value. Writes accept JSON only so a value Relay cannot decode is rejected
// rather than reinterpreted.
func (i EnqueueWorkflowInput) Validate() error {
	if i.WorkflowName == "" {
		return fmt.Errorf("workflow name is required")
	}
	if i.QueueName == "" {
		return fmt.Errorf("queue name is required")
	}
	return validateJSONPayload(i.Payload)
}

// Validate checks that the input can be written without re-encoding an opaque
// value.
func (i SendWorkflowMessageInput) Validate() error {
	if i.DestinationID == "" {
		return fmt.Errorf("destination workflow ID is required")
	}
	return validateJSONPayload(i.Payload)
}

func validateJSONPayload(payload json.RawMessage) error {
	if len(payload) == 0 {
		return fmt.Errorf("payload is required")
	}
	if !json.Valid(payload) {
		return fmt.Errorf("payload must be valid JSON")
	}
	return nil
}

// NonDatabaseReason explains why an operation has no database path. It returns
// an empty string when the data plane handles the operation. The reasons are
// user-visible so a Direct-only deployment can tell which operations need a
// live executor and which one Direct mode refuses outright.
//
// The Go SDK exposes keyed event and stream reads, not listings, and it has no
// export or import operation. Relay does not read the system tables directly,
// so these operations stay off the database path until the MIT client publishes
// a suitable operation.
func NonDatabaseReason(msgType protocol.MessageType) string {
	switch msgType {
	case protocol.MessageTypeGetWorkflowEvents:
		return "workflow event listing requires a live executor: the MIT Go client reads events by key and has no event listing operation"
	case protocol.MessageTypeGetWorkflowNotifications:
		return "workflow notification listing requires a live executor: the MIT Go client has no notification listing operation"
	case protocol.MessageTypeGetWorkflowStreams:
		return "workflow stream listing requires a live executor: the MIT Go client reads streams by key and has no stream listing operation"
	case protocol.MessageTypeExportWorkflow:
		return "workflow export requires a live executor: the serialized workflow format belongs to the executor and the MIT Go client has no export operation"
	case protocol.MessageTypeImportWorkflow:
		return "workflow import is not supported in Direct mode: the MIT Go client has no import operation and Direct mode does not retry a mutation over an executor socket"
	default:
		return ""
	}
}

// Client represents the data-plane interface to an application's system database.
type Client interface {
	Dispatch(ctx context.Context, msg protocol.Message) (protocol.Message, error)
	Enqueue(ctx context.Context, input EnqueueWorkflowInput) (EnqueueWorkflowResult, error)
	Send(ctx context.Context, input SendWorkflowMessageInput) error
	Recover(ctx context.Context, executorIDs []string, appVersion string) ([]string, error)
	HasPendingWorkflows(ctx context.Context, executorID, appVersion string) (bool, error)
	Close() error
}

// Manager manages per-application data-plane client pools and routing.
type Manager interface {
	RegisterApp(config AppConfig) error
	UnregisterApp(appID pgtype.UUID)
	HasDataPlane(appID pgtype.UUID) bool
	GetMode(appID pgtype.UUID) (Mode, bool)
	GetIntegration(appID pgtype.UUID) (Integration, bool)
	Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
	Enqueue(ctx context.Context, appID pgtype.UUID, input EnqueueWorkflowInput) (EnqueueWorkflowResult, error)
	Send(ctx context.Context, appID pgtype.UUID, input SendWorkflowMessageInput) error
	Recover(ctx context.Context, appID pgtype.UUID, executorIDs []string, appVersion string) ([]string, error)
	HasPendingWorkflows(ctx context.Context, appID pgtype.UUID, executorID, appVersion string) (bool, error)
	Close() error
}
