package protocol

import "time"

// Heartbeat timing constants for the executor WebSocket protocol.
// Provenance: Go SDK dbos-transact-go/dbos/conductor.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
const (
	DefaultPingInterval = 10 * time.Second
	DefaultPongDeadline = 25 * time.Second
)

// MessageType represents the type of message exchanged with the conductor.
type MessageType string

const (
	MessageTypeExecutorInfo                MessageType = "executor_info"
	MessageTypeRecovery                    MessageType = "recovery"
	MessageTypeCancel                      MessageType = "cancel"
	MessageTypeResume                      MessageType = "resume"
	MessageTypeListWorkflows               MessageType = "list_workflows"
	MessageTypeListQueuedWorkflows         MessageType = "list_queued_workflows"
	MessageTypeListSteps                   MessageType = "list_steps"
	MessageTypeGetWorkflow                 MessageType = "get_workflow"
	MessageTypeForkWorkflow                MessageType = "fork_workflow"
	MessageTypeForkFromFailure             MessageType = "fork_from_failure"
	MessageTypeExistPendingWorkflows       MessageType = "exist_pending_workflows"
	MessageTypeRetention                   MessageType = "retention"
	MessageTypeGetMetrics                  MessageType = "get_metrics"
	MessageTypeExportWorkflow              MessageType = "export_workflow"
	MessageTypeImportWorkflow              MessageType = "import_workflow"
	MessageTypeDelete                      MessageType = "delete"
	MessageTypeAlert                       MessageType = "alert"
	MessageTypeListSchedules               MessageType = "list_schedules"
	MessageTypeGetSchedule                 MessageType = "get_schedule"
	MessageTypePauseSchedule               MessageType = "pause_schedule"
	MessageTypeResumeSchedule              MessageType = "resume_schedule"
	MessageTypeBackfillSchedule            MessageType = "backfill_schedule"
	MessageTypeTriggerSchedule             MessageType = "trigger_schedule"
	MessageTypeGetWorkflowEvents           MessageType = "get_workflow_events"
	MessageTypeGetWorkflowNotifications    MessageType = "get_workflow_notifications"
	MessageTypeGetWorkflowStreams          MessageType = "get_workflow_streams"
	MessageTypeGetWorkflowAggregates       MessageType = "get_workflow_aggregates"
	MessageTypeGetStepAggregates           MessageType = "get_step_aggregates"
	MessageTypeListApplicationVersions     MessageType = "list_application_versions"
	MessageTypeSetLatestApplicationVersion MessageType = "set_latest_application_version"
	MessageTypeListQueues                  MessageType = "list_queues"
	MessageTypeGetQueue                    MessageType = "get_queue"
)

// Envelope represents the common structure of all conductor messages.
type Envelope struct {
	Type         MessageType `json:"type"`
	RequestID    string      `json:"request_id"`
	ErrorMessage *string     `json:"error_message,omitempty"`
}

func (e *Envelope) GetMessageType() MessageType {
	return e.Type
}

func (e *Envelope) GetRequestID() string {
	return e.RequestID
}

// Message is the interface implemented by all request and response types.
type Message interface {
	GetMessageType() MessageType
	GetRequestID() string
}
