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
		if err := cli.CancelWorkflow(cli, req.WorkflowID); err != nil {
			return nil, fmt.Errorf("failed to cancel workflow %s: %w", req.WorkflowID, err)
		}
		return &protocol.CancelWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeCancel,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil

	case *protocol.ResumeWorkflowRequest:
		if _, err := cli.ResumeWorkflow(cli, req.WorkflowID); err != nil {
			return nil, fmt.Errorf("failed to resume workflow %s: %w", req.WorkflowID, err)
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
	if s.WasForkedFrom {
		b := true
		body.WasForkedFrom = &b
	}
	if s.ParentWorkflowID != "" {
		body.ParentWorkflowID = &s.ParentWorkflowID
	}
	if !s.CompletedAt.IsZero() {
		str := strconv.FormatInt(s.CompletedAt.UnixMilli(), 10)
		body.CompletedAt = &str
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
