package dataplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"

	"github.com/abn/relay/internal/protocol"
)

// SDKClient wraps the official DBOS Go SDK client behind the Relay dataplane.Client interface.
type SDKClient struct {
	config AppConfig
	client dbos.Client
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

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	appName := cfg.ApplicationName
	if appName == "" {
		appName = "relay-dataplane"
	}

	client, err := dbos.NewClient(ctx, dbos.ClientConfig{
		DatabaseURL:            cfg.DatabaseURL,
		AppName:                appName,
		SystemDBStartupTimeout: timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create dbos client: %w", err)
	}

	return &SDKClient{
		config: cfg,
		client: client,
	}, nil
}

// Dispatch executes v1 data-plane operations against the application system database.
func (c *SDKClient) Dispatch(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
	switch req := msg.(type) {
	case *protocol.GetWorkflowRequest:
		statuses, err := c.client.ListWorkflows(c.client,
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
		if req.Body.LoadInput {
			opts = append(opts, dbos.WithFilterLoadInput(true))
		}
		if req.Body.LoadOutput {
			opts = append(opts, dbos.WithFilterLoadOutput(true))
		}
		if len(req.Body.WorkflowUUIDs) > 0 {
			opts = append(opts, dbos.WithFilterWorkflowIDs(req.Body.WorkflowUUIDs...))
		}
		if req.Body.Limit != nil && *req.Body.Limit > 0 {
			opts = append(opts, dbos.WithFilterLimit(*req.Body.Limit))
		}
		if req.Body.Offset != nil && *req.Body.Offset > 0 {
			opts = append(opts, dbos.WithFilterOffset(*req.Body.Offset))
		}
		if req.Body.SortDesc {
			opts = append(opts, dbos.WithFilterSortDesc())
		}
		statuses, err := c.client.ListWorkflows(c.client, opts...)
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
		steps, err := c.client.GetWorkflowSteps(c.client, req.WorkflowID, stepOpts...)
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
		if err := c.client.CancelWorkflow(c.client, req.WorkflowID); err != nil {
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
		if _, err := c.client.ResumeWorkflow(c.client, req.WorkflowID); err != nil {
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
		handle, err := c.client.ForkWorkflow(c.client, input)
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
	if c.client != nil {
		return c.client.Shutdown(c.client, 5*time.Second)
	}
	return nil
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
