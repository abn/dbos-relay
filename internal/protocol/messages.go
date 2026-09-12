package protocol

import (
	"encoding/json"
	"time"
)

// Provenance: Go SDK: dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)

// StringOrList is a custom JSON type that accepts either a single string
// or an array of strings, matching the conductor's StringOrList for filter fields.
type StringOrList []string

func (s *StringOrList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = nil
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = StringOrList{single}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	*s = StringOrList(list)
	return nil
}

// ExecutorInfoRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ExecutorInfoRequest struct {
	Envelope
}

// ExecutorInfoResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ExecutorInfoResponse struct {
	Envelope
	ExecutorID         string         `json:"executor_id"`
	ApplicationVersion string         `json:"application_version"`
	Hostname           *string        `json:"hostname,omitempty"`
	DBOSVersion        string         `json:"dbos_version"`
	Language           string         `json:"language"`
	ExecutorMetadata   map[string]any `json:"executor_metadata,omitempty"`
}

// ListWorkflowsRequestBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListWorkflowsRequestBody struct {
	WorkflowUUIDs      []string       `json:"workflow_uuids,omitempty"`
	WorkflowName       StringOrList   `json:"workflow_name,omitempty"`
	AuthenticatedUser  StringOrList   `json:"authenticated_user,omitempty"`
	StartTime          *time.Time     `json:"start_time,omitempty"`       // ISO 8601
	EndTime            *time.Time     `json:"end_time,omitempty"`         // ISO 8601
	CompletedAfter     *time.Time     `json:"completed_after,omitempty"`  // ISO 8601
	CompletedBefore    *time.Time     `json:"completed_before,omitempty"` // ISO 8601
	DequeuedAfter      *time.Time     `json:"dequeued_after,omitempty"`   // ISO 8601
	DequeuedBefore     *time.Time     `json:"dequeued_before,omitempty"`  // ISO 8601
	Status             StringOrList   `json:"status,omitempty"`
	ApplicationVersion StringOrList   `json:"application_version,omitempty"`
	ForkedFrom         StringOrList   `json:"forked_from,omitempty"`
	ParentWorkflowID   StringOrList   `json:"parent_workflow_id,omitempty"`
	WasForkedFrom      *bool          `json:"was_forked_from,omitempty"`
	HasParent          *bool          `json:"has_parent,omitempty"`
	QueueName          StringOrList   `json:"queue_name,omitempty"`
	Limit              *int           `json:"limit,omitempty"`
	Offset             *int           `json:"offset,omitempty"`
	SortDesc           bool           `json:"sort_desc"`
	WorkflowIDPrefix   StringOrList   `json:"workflow_id_prefix,omitempty"`
	LoadInput          bool           `json:"load_input"`
	LoadOutput         bool           `json:"load_output"`
	ExecutorID         StringOrList   `json:"executor_id,omitempty"`
	QueuesOnly         bool           `json:"queues_only"`
	Attributes         map[string]any `json:"attributes,omitempty"`
	ScheduleName       StringOrList   `json:"schedule_name,omitempty"`
	ApplicationName    StringOrList   `json:"application_name,omitempty"`
}

// ListWorkflowsRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListWorkflowsRequest struct {
	Envelope
	Body ListWorkflowsRequestBody `json:"body"`
}

// ListWorkflowsResponseBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListWorkflowsResponseBody struct {
	WorkflowUUID            string  `json:"WorkflowUUID"`
	Status                  *string `json:"Status,omitempty"`
	WorkflowName            *string `json:"WorkflowName,omitempty"`
	WorkflowClassName       *string `json:"WorkflowClassName,omitempty"`
	WorkflowConfigName      *string `json:"WorkflowConfigName,omitempty"`
	AuthenticatedUser       *string `json:"AuthenticatedUser,omitempty"`
	AssumedRole             *string `json:"AssumedRole,omitempty"`
	AuthenticatedRoles      *string `json:"AuthenticatedRoles,omitempty"`
	Input                   *string `json:"Input,omitempty"`
	Output                  *string `json:"Output,omitempty"`
	Error                   *string `json:"Error,omitempty"`
	CreatedAt               *string `json:"CreatedAt,omitempty"`
	UpdatedAt               *string `json:"UpdatedAt,omitempty"`
	QueueName               *string `json:"QueueName,omitempty"`
	ApplicationVersion      *string `json:"ApplicationVersion,omitempty"`
	ExecutorID              *string `json:"ExecutorID,omitempty"`
	WorkflowTimeoutMS       *string `json:"WorkflowTimeoutMS,omitempty"`
	WorkflowDeadlineEpochMS *string `json:"WorkflowDeadlineEpochMS,omitempty"`
	DeduplicationID         *string `json:"DeduplicationID,omitempty"`
	Priority                *string `json:"Priority,omitempty"`
	QueuePartitionKey       *string `json:"QueuePartitionKey,omitempty"`
	ForkedFrom              *string `json:"ForkedFrom,omitempty"`
	WasForkedFrom           *bool   `json:"WasForkedFrom,omitempty"`
	ParentWorkflowID        *string `json:"ParentWorkflowID,omitempty"`
	DequeuedAt              *string `json:"DequeuedAt,omitempty"`
	DelayUntilEpochMS       *string `json:"DelayUntilEpochMS,omitempty"`
	CompletedAt             *string `json:"CompletedAt,omitempty"`
	Attributes              *string `json:"Attributes,omitempty"`
	ScheduleName            *string `json:"ScheduleName,omitempty"`
	ApplicationName         *string `json:"ApplicationName,omitempty"`
}

// ListWorkflowsResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListWorkflowsResponse struct {
	Envelope
	Output []ListWorkflowsResponseBody `json:"output"`
}

// ListStepsRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListStepsRequest struct {
	Envelope
	WorkflowID string `json:"workflow_id"`
	LoadOutput bool   `json:"load_output"`
	Limit      *int   `json:"limit,omitempty"`
	Offset     *int   `json:"offset,omitempty"`
}

// WorkflowStepsResponseBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type WorkflowStepsResponseBody struct {
	FunctionID         int     `json:"function_id"`
	FunctionName       string  `json:"function_name"`
	Output             *string `json:"output,omitempty"`
	Error              *string `json:"error,omitempty"`
	ChildWorkflowID    *string `json:"child_workflow_id,omitempty"`
	StartedAtEpochMs   *string `json:"started_at_epoch_ms,omitempty"`
	CompletedAtEpochMs *string `json:"completed_at_epoch_ms,omitempty"`
}

// ListStepsResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListStepsResponse struct {
	Envelope
	Output *[]WorkflowStepsResponseBody `json:"output,omitempty"`
}

// GetWorkflowRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowRequest struct {
	Envelope
	WorkflowID string `json:"workflow_id"`
	LoadInput  bool   `json:"load_input"`
	LoadOutput bool   `json:"load_output"`
}

// GetWorkflowResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowResponse struct {
	Envelope
	Output *ListWorkflowsResponseBody `json:"output,omitempty"`
}

// ForkWorkflowRequestBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ForkWorkflowRequestBody struct {
	WorkflowID         string  `json:"workflow_id"`
	StartStep          int     `json:"start_step"`
	ApplicationVersion *string `json:"application_version"`
	NewWorkflowID      *string `json:"new_workflow_id"`
	QueueName          *string `json:"queue_name,omitempty"`
	QueuePartitionKey  *string `json:"queue_partition_key,omitempty"`
}

// ForkWorkflowRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ForkWorkflowRequest struct {
	Envelope
	Body ForkWorkflowRequestBody `json:"body"`
}

// ForkWorkflowResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ForkWorkflowResponse struct {
	Envelope
	NewWorkflowID *string `json:"new_workflow_id,omitempty"`
}

// ForkFromFailureRequestBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ForkFromFailureRequestBody struct {
	WorkflowIDs        []string `json:"workflow_ids"`
	ApplicationVersion *string  `json:"application_version,omitempty"`
	QueueName          *string  `json:"queue_name,omitempty"`
	QueuePartitionKey  *string  `json:"queue_partition_key,omitempty"`
	FromLastFailure    bool     `json:"from_last_failure,omitempty"`
	FromLastStep       bool     `json:"from_last_step,omitempty"`
	FromStep           *int     `json:"from_step,omitempty"`
	FromStepName       *string  `json:"from_step_name,omitempty"`
}

// ForkFromFailureRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ForkFromFailureRequest struct {
	Envelope
	Body ForkFromFailureRequestBody `json:"body"`
}

// ForkFromFailureResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ForkFromFailureResponse struct {
	Envelope
	ForkedWorkflowIDs []string `json:"forked_workflow_ids,omitempty"`
}

// CancelWorkflowRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type CancelWorkflowRequest struct {
	Envelope
	CancelChildren bool     `json:"cancel_children"`
	WorkflowID     string   `json:"workflow_id"`
	WorkflowIDs    []string `json:"workflow_ids"`
}

// CancelWorkflowResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type CancelWorkflowResponse struct {
	Envelope
	Success bool `json:"success"`
}

// RecoveryRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type RecoveryRequest struct {
	Envelope
	ExecutorIDs []string `json:"executor_ids"`
}

// RecoveryResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type RecoveryResponse struct {
	Envelope
	Success bool `json:"success"`
}

// ExistPendingWorkflowsRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ExistPendingWorkflowsRequest struct {
	Envelope
	ExecutorID         string `json:"executor_id"`
	ApplicationVersion string `json:"application_version"`
}

// ExistPendingWorkflowsResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ExistPendingWorkflowsResponse struct {
	Envelope
	Exist bool `json:"exist"`
}

// ResumeWorkflowRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ResumeWorkflowRequest struct {
	Envelope
	WorkflowID  string   `json:"workflow_id"`
	WorkflowIDs []string `json:"workflow_ids"`
	QueueName   *string  `json:"queue_name,omitempty"`
}

// ResumeWorkflowResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ResumeWorkflowResponse struct {
	Envelope
	Success bool `json:"success"`
}

// RetentionRequestBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type RetentionRequestBody struct {
	GCCutoffEpochMs      *int `json:"gc_cutoff_epoch_ms,omitempty"`
	GCRowsThreshold      *int `json:"gc_rows_threshold,omitempty"`
	GCBatchSize          *int `json:"gc_batch_size,omitempty"`
	TimeoutCutoffEpochMs *int `json:"timeout_cutoff_epoch_ms,omitempty"`
}

// RetentionRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type RetentionRequest struct {
	Envelope
	Body RetentionRequestBody `json:"body"`
}

// RetentionResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type RetentionResponse struct {
	Envelope
	Success bool `json:"success"`
}

// GetMetricsRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetMetricsRequest struct {
	Envelope
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	MetricClass string `json:"metric_class"`
	// Absent
	ApplicationName []string `json:"application_name,omitempty"`
}

// GetMetricsResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetMetricsResponse struct {
	Envelope
	Metrics []MetricData `json:"metrics"`
}

// ExportWorkflowRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ExportWorkflowRequest struct {
	Envelope
	WorkflowID     string `json:"workflow_id"`
	ExportChildren bool   `json:"export_children"`
}

// ExportWorkflowResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ExportWorkflowResponse struct {
	Envelope
	SerializedWorkflow *string `json:"serialized_workflow,omitempty"`
}

// ImportWorkflowRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ImportWorkflowRequest struct {
	Envelope
	SerializedWorkflow string `json:"serialized_workflow"`
}

// ImportWorkflowResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ImportWorkflowResponse struct {
	Envelope
	Success bool `json:"success"`
}

// DeleteWorkflowRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type DeleteWorkflowRequest struct {
	Envelope
	WorkflowID     string   `json:"workflow_id"`
	WorkflowIDs    []string `json:"workflow_ids"`
	DeleteChildren bool     `json:"delete_children"`
}

// DeleteWorkflowResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type DeleteWorkflowResponse struct {
	Envelope
	Success bool `json:"success"`
}

// AlertRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type AlertRequest struct {
	Envelope
	Name     string            `json:"name"`
	Message  string            `json:"message"`
	Metadata map[string]string `json:"metadata"`
}

// AlertResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type AlertResponse struct {
	Envelope
	Success bool `json:"success"`
}

// ScheduleOutput provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ScheduleOutput struct {
	ScheduleID        string  `json:"schedule_id"`
	ScheduleName      string  `json:"schedule_name"`
	WorkflowName      string  `json:"workflow_name"`
	WorkflowClassName *string `json:"workflow_class_name"`
	Schedule          string  `json:"schedule"`
	Status            string  `json:"status"`
	Context           *string `json:"context"`
	LastFiredAt       *string `json:"last_fired_at"`
	AutomaticBackfill bool    `json:"automatic_backfill"`
	CronTimezone      *string `json:"cron_timezone"`
	QueueName         *string `json:"queue_name"`
	ApplicationName   *string `json:"application_name"`
}

// ListSchedulesRequestBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListSchedulesRequestBody struct {
	Status             StringOrList `json:"status,omitempty"`
	WorkflowName       StringOrList `json:"workflow_name,omitempty"`
	ScheduleNamePrefix StringOrList `json:"schedule_name_prefix,omitempty"`
	ApplicationName    StringOrList `json:"application_name,omitempty"`
	LoadContext        *bool        `json:"load_context,omitempty"`
}

// ListSchedulesRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListSchedulesRequest struct {
	Envelope
	Body ListSchedulesRequestBody `json:"body"`
}

// ListSchedulesResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListSchedulesResponse struct {
	Envelope
	Output []ScheduleOutput `json:"output"`
}

// GetScheduleRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetScheduleRequest struct {
	Envelope
	ScheduleName string `json:"schedule_name"`
	LoadContext  *bool  `json:"load_context,omitempty"`
}

// GetScheduleResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetScheduleResponse struct {
	Envelope
	Output *ScheduleOutput `json:"output"`
}

// PauseScheduleRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type PauseScheduleRequest struct {
	Envelope
	ScheduleName string `json:"schedule_name"`
}

// PauseScheduleResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type PauseScheduleResponse struct {
	Envelope
	Success bool `json:"success"`
}

// ResumeScheduleRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ResumeScheduleRequest struct {
	Envelope
	ScheduleName string `json:"schedule_name"`
}

// ResumeScheduleResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ResumeScheduleResponse struct {
	Envelope
	Success bool `json:"success"`
}

// BackfillScheduleRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type BackfillScheduleRequest struct {
	Envelope
	ScheduleName string `json:"schedule_name"`
	Start        string `json:"start"` // ISO 8601
	End          string `json:"end"`   // ISO 8601
}

// BackfillScheduleResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type BackfillScheduleResponse struct {
	Envelope
	WorkflowIDs []string `json:"workflow_ids"`
}

// TriggerScheduleRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type TriggerScheduleRequest struct {
	Envelope
	ScheduleName string `json:"schedule_name"`
}

// TriggerScheduleResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type TriggerScheduleResponse struct {
	Envelope
	WorkflowID *string `json:"workflow_id"`
}

// QueueOutput provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type QueueOutput struct {
	Name               string   `json:"name"`
	Concurrency        *int     `json:"concurrency"`
	WorkerConcurrency  *int     `json:"worker_concurrency"`
	RateLimitMax       *int     `json:"rate_limit_max"`
	RateLimitPeriodSec *float64 `json:"rate_limit_period_sec"`
	PriorityEnabled    bool     `json:"priority_enabled"`
	PartitionQueue     bool     `json:"partition_queue"`
	PollingIntervalSec float64  `json:"polling_interval_sec"`
	ApplicationName    *string  `json:"application_name"`
}

// ListQueuesRequestBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListQueuesRequestBody struct {
	ApplicationName StringOrList `json:"application_name,omitempty"`
}

// ListQueuesRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListQueuesRequest struct {
	Envelope
	// Absent
	Body ListQueuesRequestBody `json:"body,omitempty"`
}

// ListQueuesResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListQueuesResponse struct {
	Envelope
	Output []QueueOutput `json:"output"`
}

// GetQueueRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetQueueRequest struct {
	Envelope
	Name string `json:"name"`
}

// GetQueueResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetQueueResponse struct {
	Envelope
	Output *QueueOutput `json:"output"`
}

// EventOutput provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type EventOutput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// NotificationOutput provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type NotificationOutput struct {
	Topic            *string `json:"topic"`
	Message          string  `json:"message"`
	CreatedAtEpochMs int64   `json:"created_at_epoch_ms"`
	Consumed         bool    `json:"consumed"`
}

// StreamEntryOutput provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type StreamEntryOutput struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

// GetWorkflowEventsRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowEventsRequest struct {
	Envelope
	WorkflowID string `json:"workflow_id"`
}

// GetWorkflowEventsResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowEventsResponse struct {
	Envelope
	Events []EventOutput `json:"events"`
}

// GetWorkflowNotificationsRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowNotificationsRequest struct {
	Envelope
	WorkflowID string `json:"workflow_id"`
}

// GetWorkflowNotificationsResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowNotificationsResponse struct {
	Envelope
	Notifications []NotificationOutput `json:"notifications"`
}

// GetWorkflowStreamsRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowStreamsRequest struct {
	Envelope
	WorkflowID string `json:"workflow_id"`
}

// GetWorkflowStreamsResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowStreamsResponse struct {
	Envelope
	Streams []StreamEntryOutput `json:"streams"`
}

// GetWorkflowAggregatesRequestBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowAggregatesRequestBody struct {
	GroupByStatus             bool           `json:"group_by_status"`
	GroupByName               bool           `json:"group_by_name"`
	GroupByQueueName          bool           `json:"group_by_queue_name"`
	GroupByExecutorID         bool           `json:"group_by_executor_id"`
	GroupByApplicationVersion bool           `json:"group_by_application_version"`
	GroupByApplicationName    bool           `json:"group_by_application_name"`
	SelectCount               bool           `json:"select_count"`
	SelectMinCreatedAt        bool           `json:"select_min_created_at"`
	SelectMaxQueueWaitMs      bool           `json:"select_max_queue_wait_ms"`
	SelectMaxTotalLatencyMs   bool           `json:"select_max_total_latency_ms"`
	TimeBucketSizeMs          *int64         `json:"time_bucket_size_ms,omitempty"`
	Status                    StringOrList   `json:"status,omitempty"`
	StartTime                 *time.Time     `json:"start_time,omitempty"`       // ISO 8601
	EndTime                   *time.Time     `json:"end_time,omitempty"`         // ISO 8601
	CompletedAfter            *time.Time     `json:"completed_after,omitempty"`  // ISO 8601
	CompletedBefore           *time.Time     `json:"completed_before,omitempty"` // ISO 8601
	DequeuedAfter             *time.Time     `json:"dequeued_after,omitempty"`   // ISO 8601
	DequeuedBefore            *time.Time     `json:"dequeued_before,omitempty"`  // ISO 8601
	Name                      StringOrList   `json:"name,omitempty"`
	AppVersion                StringOrList   `json:"app_version,omitempty"`
	ExecutorID                StringOrList   `json:"executor_id,omitempty"`
	QueueName                 StringOrList   `json:"queue_name,omitempty"`
	WorkflowIDPrefix          StringOrList   `json:"workflow_id_prefix,omitempty"`
	WorkflowIDs               StringOrList   `json:"workflow_ids,omitempty"`
	ForkedFrom                StringOrList   `json:"forked_from,omitempty"`
	ParentWorkflowID          StringOrList   `json:"parent_workflow_id,omitempty"`
	User                      StringOrList   `json:"user,omitempty"`
	ApplicationName           StringOrList   `json:"application_name,omitempty"`
	WasForkedFrom             *bool          `json:"was_forked_from,omitempty"`
	HasParent                 *bool          `json:"has_parent,omitempty"`
	Attributes                map[string]any `json:"attributes,omitempty"`
}

// GetWorkflowAggregatesRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowAggregatesRequest struct {
	Envelope
	Body GetWorkflowAggregatesRequestBody `json:"body"`
}

// WorkflowAggregateRow mirrors the Go SDK dbos.WorkflowAggregateRow shape.
// Provenance: dbos-inc/dbos-transact-golang dbos/internal/sysdb/system_database.go:3414 (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type WorkflowAggregateRow struct {
	Group             map[string]*string `json:"group"`
	Count             *int64             `json:"count"`
	MinCreatedAt      *int64             `json:"min_created_at"`
	MaxQueueWaitMs    *int64             `json:"max_queue_wait_ms"`
	MaxTotalLatencyMs *int64             `json:"max_total_latency_ms"`
}

// GetWorkflowAggregatesResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetWorkflowAggregatesResponse struct {
	Envelope
	Output []WorkflowAggregateRow `json:"output"`
}

// StepAggregateRow mirrors the Go SDK dbos.StepAggregateRow shape.
// Provenance: dbos-inc/dbos-transact-golang dbos/internal/sysdb/system_database.go:3708 (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type StepAggregateRow struct {
	Group         map[string]*string `json:"group"`
	Count         *int64             `json:"count"`
	MaxDurationMs *int64             `json:"max_duration_ms"`
}

// GetStepAggregatesRequestBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetStepAggregatesRequestBody struct {
	GroupByFunctionName bool         `json:"group_by_function_name"`
	GroupByStatus       bool         `json:"group_by_status"`
	SelectCount         bool         `json:"select_count"`
	SelectMaxDurationMs bool         `json:"select_max_duration_ms"`
	TimeBucketSizeMs    *int64       `json:"time_bucket_size_ms,omitempty"`
	Status              StringOrList `json:"status,omitempty"`
	FunctionName        StringOrList `json:"function_name,omitempty"`
	WorkflowIDPrefix    StringOrList `json:"workflow_id_prefix,omitempty"`
	CompletedAfter      *time.Time   `json:"completed_after,omitempty"`  // ISO 8601
	CompletedBefore     *time.Time   `json:"completed_before,omitempty"` // ISO 8601
	ApplicationName     StringOrList `json:"application_name,omitempty"`
}

// GetStepAggregatesRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetStepAggregatesRequest struct {
	Envelope
	Body GetStepAggregatesRequestBody `json:"body"`
}

// GetStepAggregatesResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type GetStepAggregatesResponse struct {
	Envelope
	Output []StepAggregateRow `json:"output"`
}

// ApplicationVersionOutput provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ApplicationVersionOutput struct {
	ID        string `json:"version_id"`
	Name      string `json:"version_name"`
	Timestamp int64  `json:"version_timestamp"`
	CreatedAt int64  `json:"created_at"`
}

// ListApplicationVersionsRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListApplicationVersionsRequest struct {
	Envelope
}

// ListApplicationVersionsResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type ListApplicationVersionsResponse struct {
	Envelope
	Output []ApplicationVersionOutput `json:"output"`
}

// SetLatestApplicationVersionRequest provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type SetLatestApplicationVersionRequest struct {
	Envelope
	VersionName string `json:"version_name"`
}

// SetLatestApplicationVersionResponse provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type SetLatestApplicationVersionResponse struct {
	Envelope
	Success bool `json:"success"`
}

// MetricData provenance: Go SDK dbos-transact-go/dbos/internal/sysdb/system_database.go:5449 (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8)
type MetricData struct {
	MetricName string  `json:"metric_name"`
	MetricType string  `json:"metric_type"`
	Value      float64 `json:"value"`

	// Deprecated fields retained for compatibility with internal/api
	MetricValue float64        `json:"metric_value,omitempty"`
	Timestamp   int64          `json:"timestamp,omitempty"`
	Tags        map[string]any `json:"tags,omitempty"`
}
