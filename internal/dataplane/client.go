package dataplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"

	"github.com/abn/relay/internal/protocol"
)

// SDKClient wraps the official DBOS Go SDK client behind the Relay dataplane.Client interface.
type SDKClient struct {
	config AppConfig
	client dbos.Client
	cancel context.CancelFunc
}

// NewSDKClient initializes an SDKClient backed by dbos.NewClient.
// Provenance: dbos-inc/dbos-transact-golang (commit ab56911fdd78552e1e7fe648cff7c831a1e760c8, dbos/dbos.go:742)
func NewSDKClient(cfg AppConfig) (Client, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}

	timeout := cfg.StatementTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	appName := cfg.ApplicationName
	if appName == "" {
		appName = "relay-dataplane"
	}

	u, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}
	q := u.Query()
	if cfg.MaxConnections > 0 {
		q.Set("pool_max_conns", strconv.Itoa(cfg.MaxConnections))
	}
	if timeout > 0 {
		q.Set("statement_timeout", strconv.FormatInt(timeout.Milliseconds(), 10))
	}
	u.RawQuery = q.Encode()
	dbURL := u.String()

	client, err := dbos.NewClient(ctx, dbos.ClientConfig{
		DatabaseURL:            dbURL,
		AppName:                appName,
		SystemDBStartupTimeout: timeout,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create dbos client: %w", err)
	}

	return &SDKClient{
		config: cfg,
		client: client,
		cancel: cancel,
	}, nil
}

// Dispatch executes v1 data-plane operations against the application system database.
func (c *SDKClient) Dispatch(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
	cli := dbos.From(c.client.(dbos.Context), ctx)
	switch req := msg.(type) {
	case *protocol.GetWorkflowRequest:
		statuses, err := cli.ListWorkflows(cli,
			dbos.WithFilterWorkflowIDs(req.WorkflowID),
			dbos.WithFilterLoadInput(true),
			dbos.WithFilterLoadOutput(true),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve workflow %s: %w", req.WorkflowID, err)
		}
		if len(statuses) == 0 {
			return nil, fmt.Errorf("workflow %s not found", req.WorkflowID)
		}
		body := mapWorkflowStatus(statuses[0])
		return &protocol.GetWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetWorkflow,
				RequestID: req.RequestID,
			},
			Output: &body,
		}, nil

	case *protocol.ListWorkflowsRequest:
		var opts []dbos.ListWorkflowsOption
		if len(req.Body.WorkflowUUIDs) > 0 {
			opts = append(opts, dbos.WithFilterWorkflowIDs(req.Body.WorkflowUUIDs...))
		}
		if len(req.Body.WorkflowName) > 0 {
			opts = append(opts, dbos.WithFilterName(req.Body.WorkflowName...))
		}
		if len(req.Body.AuthenticatedUser) > 0 {
			opts = append(opts, dbos.WithFilterUser(req.Body.AuthenticatedUser...))
		}
		if req.Body.StartTime != nil {
			opts = append(opts, dbos.WithFilterCreatedAfter(*req.Body.StartTime))
		}
		if req.Body.EndTime != nil {
			opts = append(opts, dbos.WithFilterCreatedBefore(*req.Body.EndTime))
		}
		if req.Body.CompletedAfter != nil {
			opts = append(opts, dbos.WithFilterCompletedAfter(*req.Body.CompletedAfter))
		}
		if req.Body.CompletedBefore != nil {
			opts = append(opts, dbos.WithFilterCompletedBefore(*req.Body.CompletedBefore))
		}
		if req.Body.DequeuedAfter != nil {
			opts = append(opts, dbos.WithFilterDequeuedAfter(*req.Body.DequeuedAfter))
		}
		if req.Body.DequeuedBefore != nil {
			opts = append(opts, dbos.WithFilterDequeuedBefore(*req.Body.DequeuedBefore))
		}
		if len(req.Body.Status) > 0 {
			var statuses []dbos.WorkflowStatusType
			for _, s := range req.Body.Status {
				statuses = append(statuses, dbos.WorkflowStatusType(s))
			}
			opts = append(opts, dbos.WithFilterStatus(statuses...))
		}
		if len(req.Body.ApplicationVersion) > 0 {
			opts = append(opts, dbos.WithFilterAppVersion(req.Body.ApplicationVersion...))
		}
		if len(req.Body.ForkedFrom) > 0 {
			opts = append(opts, dbos.WithFilterForkedFrom(req.Body.ForkedFrom...))
		}
		if len(req.Body.ParentWorkflowID) > 0 {
			opts = append(opts, dbos.WithFilterParentWorkflowID(req.Body.ParentWorkflowID...))
		}
		if req.Body.WasForkedFrom != nil {
			opts = append(opts, dbos.WithFilterWasForkedFrom(*req.Body.WasForkedFrom))
		}
		if req.Body.HasParent != nil {
			opts = append(opts, dbos.WithFilterHasParent(*req.Body.HasParent))
		}
		if len(req.Body.QueueName) > 0 {
			opts = append(opts, dbos.WithFilterQueueName(req.Body.QueueName...))
		}
		if req.Body.Limit != nil {
			opts = append(opts, dbos.WithFilterLimit(*req.Body.Limit))
		}
		if req.Body.Offset != nil {
			opts = append(opts, dbos.WithFilterOffset(*req.Body.Offset))
		}
		if req.Body.SortDesc {
			opts = append(opts, dbos.WithFilterSortDesc())
		}
		if len(req.Body.WorkflowIDPrefix) > 0 {
			opts = append(opts, dbos.WithFilterWorkflowIDPrefix(req.Body.WorkflowIDPrefix...))
		}
		if req.Body.LoadInput {
			opts = append(opts, dbos.WithFilterLoadInput(true))
		}
		if req.Body.LoadOutput {
			opts = append(opts, dbos.WithFilterLoadOutput(true))
		}
		if len(req.Body.ExecutorID) > 0 {
			opts = append(opts, dbos.WithFilterExecutorIDs(req.Body.ExecutorID...))
		}
		if req.Body.QueuesOnly {
			opts = append(opts, dbos.WithFilterQueuesOnly())
		}
		if req.Body.Attributes != nil {
			opts = append(opts, dbos.WithFilterAttributes(req.Body.Attributes))
		}
		if len(req.Body.ScheduleName) > 0 {
			opts = append(opts, dbos.WithFilterScheduleName(req.Body.ScheduleName...))
		}
		if len(req.Body.ApplicationName) > 0 {
			opts = append(opts, dbos.WithFilterApplicationName(req.Body.ApplicationName...))
		}
		statuses, err := cli.ListWorkflows(cli, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to list workflows: %w", err)
		}
		outputs := make([]protocol.ListWorkflowsResponseBody, len(statuses))
		for i, s := range statuses {
			outputs[i] = mapWorkflowStatus(s)
		}
		return &protocol.ListWorkflowsResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListWorkflows,
				RequestID: req.RequestID,
			},
			Output: outputs,
		}, nil

	case *protocol.ListStepsRequest:
		stepOpts := []dbos.GetWorkflowStepsOption{
			dbos.WithStepsLoadOutput(req.LoadOutput),
		}
		if req.Limit != nil && *req.Limit > 0 {
			stepOpts = append(stepOpts, dbos.WithStepsLimit(*req.Limit))
		}
		steps, err := cli.GetWorkflowSteps(cli, req.WorkflowID, stepOpts...)
		if err != nil {
			return nil, fmt.Errorf("failed to get workflow steps for %s: %w", req.WorkflowID, err)
		}
		outputs := make([]protocol.WorkflowStepsResponseBody, len(steps))
		for i, s := range steps {
			outputs[i] = mapStepInfo(s)
		}
		return &protocol.ListStepsResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListSteps,
				RequestID: req.RequestID,
			},
			Output: &outputs,
		}, nil

	case *protocol.CancelWorkflowRequest:
		var opts []dbos.CancelWorkflowOption
		if req.CancelChildren {
			opts = append(opts, dbos.WithCancelChildren())
		}
		if len(req.WorkflowIDs) > 0 {
			if err := cli.CancelWorkflows(cli, req.WorkflowIDs, opts...); err != nil {
				return nil, fmt.Errorf("failed to cancel workflows: %w", err)
			}
		} else if req.WorkflowID != "" {
			if err := cli.CancelWorkflow(cli, req.WorkflowID, opts...); err != nil {
				return nil, fmt.Errorf("failed to cancel workflow %s: %w", req.WorkflowID, err)
			}
		} else {
			return nil, fmt.Errorf("missing workflow ID(s) in cancel request")
		}
		return &protocol.CancelWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeCancel,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil

	case *protocol.ResumeWorkflowRequest:
		var opts []dbos.ResumeWorkflowOption
		if req.QueueName != nil {
			opts = append(opts, dbos.WithResumeQueue(*req.QueueName))
		}
		if len(req.WorkflowIDs) > 0 {
			if _, err := cli.ResumeWorkflows(cli, req.WorkflowIDs, opts...); err != nil {
				return nil, fmt.Errorf("failed to resume workflows: %w", err)
			}
		} else if req.WorkflowID != "" {
			if _, err := cli.ResumeWorkflow(cli, req.WorkflowID, opts...); err != nil {
				return nil, fmt.Errorf("failed to resume workflow %s: %w", req.WorkflowID, err)
			}
		} else {
			return nil, fmt.Errorf("missing workflow ID(s) in resume request")
		}
		return &protocol.ResumeWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeResume,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil

	case *protocol.ForkWorkflowRequest:
		var startStep uint
		if req.Body.StartStep > 0 {
			startStep = uint(req.Body.StartStep)
		}
		input := dbos.ForkWorkflowInput{
			OriginalWorkflowID: req.Body.WorkflowID,
			StartStep:          startStep,
		}
		if req.Body.NewWorkflowID != nil {
			input.ForkedWorkflowID = *req.Body.NewWorkflowID
		}
		if req.Body.QueueName != nil {
			input.QueueName = *req.Body.QueueName
		}
		if req.Body.ApplicationVersion != nil {
			input.ApplicationVersion = *req.Body.ApplicationVersion
		}
		handle, err := cli.ForkWorkflow(cli, input)
		if err != nil {
			return nil, fmt.Errorf("failed to fork workflow %s: %w", req.Body.WorkflowID, err)
		}
		newID := handle.GetWorkflowID()
		return &protocol.ForkWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeForkWorkflow,
				RequestID: req.RequestID,
			},
			NewWorkflowID: &newID,
		}, nil

	case *protocol.GetWorkflowAggregatesRequest:
		var tb time.Duration
		if req.Body.TimeBucketSizeMs != nil {
			tb = time.Duration(*req.Body.TimeBucketSizeMs) * time.Millisecond
		}
		var statuses []dbos.WorkflowStatusType
		if len(req.Body.Status) > 0 {
			for _, st := range req.Body.Status {
				statuses = append(statuses, dbos.WorkflowStatusType(st))
			}
		}
		var startTime, endTime, completedAfter, completedBefore, dequeuedAfter, dequeuedBefore time.Time
		if req.Body.StartTime != nil {
			startTime = *req.Body.StartTime
		}
		if req.Body.EndTime != nil {
			endTime = *req.Body.EndTime
		}
		if req.Body.CompletedAfter != nil {
			completedAfter = *req.Body.CompletedAfter
		}
		if req.Body.CompletedBefore != nil {
			completedBefore = *req.Body.CompletedBefore
		}
		if req.Body.DequeuedAfter != nil {
			dequeuedAfter = *req.Body.DequeuedAfter
		}
		if req.Body.DequeuedBefore != nil {
			dequeuedBefore = *req.Body.DequeuedBefore
		}

		input := dbos.GetWorkflowAggregatesInput{
			GroupByStatus:             req.Body.GroupByStatus,
			GroupByName:               req.Body.GroupByName,
			GroupByQueueName:          req.Body.GroupByQueueName,
			GroupByExecutorID:         req.Body.GroupByExecutorID,
			GroupByApplicationVersion: req.Body.GroupByApplicationVersion,
			GroupByApplicationName:    req.Body.GroupByApplicationName,
			SelectCount:               req.Body.SelectCount,
			SelectMinCreatedAt:        req.Body.SelectMinCreatedAt,
			SelectMaxQueueWaitMs:      req.Body.SelectMaxQueueWaitMs,
			SelectMaxTotalLatencyMs:   req.Body.SelectMaxTotalLatencyMs,
			TimeBucketSize:            tb,
			Status:                    statuses,
			StartTime:                 startTime,
			EndTime:                   endTime,
			CompletedAfter:            completedAfter,
			CompletedBefore:           completedBefore,
			DequeuedAfter:             dequeuedAfter,
			DequeuedBefore:            dequeuedBefore,
			Name:                      req.Body.Name,
			ApplicationVersion:        req.Body.AppVersion,
			ExecutorID:                req.Body.ExecutorID,
			QueueName:                 req.Body.QueueName,
			WorkflowIDPrefix:          req.Body.WorkflowIDPrefix,
			WorkflowIDs:               req.Body.WorkflowIDs,
			AuthenticatedUser:         req.Body.User,
			ForkedFrom:                req.Body.ForkedFrom,
			ParentWorkflowID:          req.Body.ParentWorkflowID,
			ApplicationName:           req.Body.ApplicationName,
			WasForkedFrom:             req.Body.WasForkedFrom,
			HasParent:                 req.Body.HasParent,
			Attributes:                req.Body.Attributes,
		}
		rows, err := cli.GetWorkflowAggregates(cli, input)
		if err != nil {
			return nil, fmt.Errorf("failed to get workflow aggregates: %w", err)
		}
		outputs := make([]map[string]any, len(rows))
		for i, row := range rows {
			m := make(map[string]any)
			for k, v := range row.Group {
				if v == nil {
					m[k] = nil
				} else {
					m[k] = *v
				}
			}
			if row.Count != nil {
				m["count"] = *row.Count
			}
			if row.MinCreatedAt != nil {
				m["min_created_at"] = *row.MinCreatedAt
			}
			if row.MaxQueueWaitMs != nil {
				m["max_queue_wait_ms"] = *row.MaxQueueWaitMs
			}
			if row.MaxTotalLatencyMs != nil {
				m["max_total_latency_ms"] = *row.MaxTotalLatencyMs
			}
			outputs[i] = m
		}
		return &protocol.GetWorkflowAggregatesResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetWorkflowAggregates,
				RequestID: req.RequestID,
			},
			Output: outputs,
		}, nil

	case *protocol.GetStepAggregatesRequest:
		var tb time.Duration
		if req.Body.TimeBucketSizeMs != nil {
			tb = time.Duration(*req.Body.TimeBucketSizeMs) * time.Millisecond
		}
		var completedAfter, completedBefore time.Time
		if req.Body.CompletedAfter != nil {
			completedAfter = *req.Body.CompletedAfter
		}
		if req.Body.CompletedBefore != nil {
			completedBefore = *req.Body.CompletedBefore
		}

		input := dbos.GetStepAggregatesInput{
			GroupByFunctionName: req.Body.GroupByFunctionName,
			GroupByStatus:       req.Body.GroupByStatus,
			SelectCount:         req.Body.SelectCount,
			SelectMaxDurationMs: req.Body.SelectMaxDurationMs,
			TimeBucketSize:      tb,
			Status:              req.Body.Status,
			FunctionName:        req.Body.FunctionName,
			WorkflowIDPrefix:    req.Body.WorkflowIDPrefix,
			CompletedAfter:      completedAfter,
			CompletedBefore:     completedBefore,
			ApplicationName:     req.Body.ApplicationName,
		}
		rows, err := cli.GetStepAggregates(cli, input)
		if err != nil {
			return nil, fmt.Errorf("failed to get step aggregates: %w", err)
		}
		outputs := make([]map[string]any, len(rows))
		for i, row := range rows {
			m := make(map[string]any)
			for k, v := range row.Group {
				if v == nil {
					m[k] = nil
				} else {
					m[k] = *v
				}
			}
			if row.Count != nil {
				m["count"] = *row.Count
			}
			if row.MaxDurationMs != nil {
				m["max_duration_ms"] = *row.MaxDurationMs
			}
			outputs[i] = m
		}
		return &protocol.GetStepAggregatesResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetStepAggregates,
				RequestID: req.RequestID,
			},
			Output: outputs,
		}, nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedOperation, msg.GetMessageType())
	}
}

// Close terminates the DBOS client runtime and connections.
func (c *SDKClient) Close() error {
	var err error
	if c.client != nil {
		err = c.client.Shutdown(c.client, 5*time.Second)
	}
	if c.cancel != nil {
		c.cancel()
	}
	return err
}

func mapWorkflowStatus(s dbos.WorkflowStatus) protocol.ListWorkflowsResponseBody {
	body := protocol.ListWorkflowsResponseBody{
		WorkflowUUID: s.ID,
	}
	if s.Status != "" {
		str := string(s.Status)
		body.Status = &str
	}
	if s.Name != "" {
		body.WorkflowName = &s.Name
	}
	if s.AuthenticatedUser != "" {
		body.AuthenticatedUser = &s.AuthenticatedUser
	}
	if s.AssumedRole != "" {
		body.AssumedRole = &s.AssumedRole
	}
	if len(s.AuthenticatedRoles) > 0 {
		if b, err := json.Marshal(s.AuthenticatedRoles); err == nil {
			str := string(b)
			body.AuthenticatedRoles = &str
		}
	}
	if s.ApplicationVersion != "" {
		body.ApplicationVersion = &s.ApplicationVersion
	}
	if s.ExecutorID != "" {
		body.ExecutorID = &s.ExecutorID
	}
	if !s.CreatedAt.IsZero() {
		str := strconv.FormatInt(s.CreatedAt.UnixMilli(), 10)
		body.CreatedAt = &str
	}
	if !s.UpdatedAt.IsZero() {
		str := strconv.FormatInt(s.UpdatedAt.UnixMilli(), 10)
		body.UpdatedAt = &str
	}
	if s.QueueName != "" {
		body.QueueName = &s.QueueName
	}
	if s.ForkedFrom != "" {
		body.ForkedFrom = &s.ForkedFrom
	}
	b := s.WasForkedFrom
	body.WasForkedFrom = &b
	if s.ParentWorkflowID != "" {
		body.ParentWorkflowID = &s.ParentWorkflowID
	}
	if !s.CompletedAt.IsZero() {
		str := strconv.FormatInt(s.CompletedAt.UnixMilli(), 10)
		body.CompletedAt = &str
	}
	if s.Timeout != 0 {
		str := strconv.FormatInt(s.Timeout.Milliseconds(), 10)
		body.WorkflowTimeoutMS = &str
	}
	if !s.Deadline.IsZero() {
		str := strconv.FormatInt(s.Deadline.UnixMilli(), 10)
		body.WorkflowDeadlineEpochMS = &str
	}
	if !s.StartedAt.IsZero() {
		str := strconv.FormatInt(s.StartedAt.UnixMilli(), 10)
		body.DequeuedAt = &str
	}
	if s.DeduplicationID != "" {
		body.DeduplicationID = &s.DeduplicationID
	}
	if s.Priority != 0 {
		str := strconv.Itoa(s.Priority)
		body.Priority = &str
	}
	if s.QueuePartitionKey != "" {
		body.QueuePartitionKey = &s.QueuePartitionKey
	}
	if !s.DelayUntil.IsZero() {
		str := strconv.FormatInt(s.DelayUntil.UnixMilli(), 10)
		body.DelayUntilEpochMS = &str
	}
	if len(s.Attributes) > 0 {
		if bAttr, err := json.Marshal(s.Attributes); err == nil {
			str := string(bAttr)
			body.Attributes = &str
		}
	}
	if s.ScheduleName != "" {
		body.ScheduleName = &s.ScheduleName
	}
	if s.ApplicationName != "" {
		body.ApplicationName = &s.ApplicationName
	}
	if s.Input != nil {
		if str, ok := listingValueJSON(s.Input); ok {
			body.Input = &str
		}
	}
	if s.Output != nil {
		if str, ok := listingValueJSON(s.Output); ok {
			body.Output = &str
		}
	}
	if s.Error != nil {
		str := s.Error.Error()
		body.Error = &str
	}
	return body
}

func mapStepInfo(s dbos.StepInfo) protocol.WorkflowStepsResponseBody {
	body := protocol.WorkflowStepsResponseBody{
		FunctionID:   s.StepID,
		FunctionName: s.StepName,
	}
	if s.ChildWorkflowID != "" {
		body.ChildWorkflowID = &s.ChildWorkflowID
	}
	if !s.StartedAt.IsZero() {
		str := strconv.FormatInt(s.StartedAt.UnixMilli(), 10)
		body.StartedAtEpochMs = &str
	}
	if !s.CompletedAt.IsZero() {
		str := strconv.FormatInt(s.CompletedAt.UnixMilli(), 10)
		body.CompletedAtEpochMs = &str
	}
	if s.Output != nil {
		if str, ok := listingValueJSON(s.Output); ok {
			body.Output = &str
		}
	}
	if s.Error != nil {
		str := s.Error.Error()
		body.Error = &str
	}
	return body
}

func listingValueJSON(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	if s, ok := v.(string); ok && json.Valid([]byte(s)) {
		return s, true
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", false
	}
	return string(b), true
}
