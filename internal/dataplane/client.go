package dataplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	_ "github.com/dbos-inc/dbos-transact-golang/dbos/driver/sqlite"
	"github.com/dbos-inc/dbos-transact-golang/dbos/internals"

	"github.com/abn/relay/internal/protocol"
)

// SDKClient wraps the official DBOS Go SDK client behind the Relay dataplane.Client interface.
type SDKClient struct {
	config AppConfig
	client dbos.Client
	cancel context.CancelFunc
}

// NewSDKClient initializes an SDKClient backed by dbos.NewClient.
// Provenance: dbos-inc/dbos-transact-golang (commit fb3e33e0b4c3c709b9271eb935adce5eaf9386f5, dbos/dbos.go:779)
// Note: Upstream DBOS Go SDK client requires system database schema version 123 or higher.
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

	startupTimeout := 30 * time.Second
	if timeout > startupTimeout {
		startupTimeout = timeout
	}

	client, err := dbos.NewClient(ctx, dbos.ClientConfig{
		DatabaseURL:            dbURL,
		AppName:                appName,
		SystemDBStartupTimeout: startupTimeout,
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
	dbosCtx, ok := c.client.(dbos.Context)
	if !ok {
		return nil, fmt.Errorf("data-plane client does not expose a DBOS context")
	}
	cli := dbos.From(dbosCtx, ctx)
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
		if req.Body.IsFork != nil {
			opts = append(opts, dbos.WithFilterIsFork(*req.Body.IsFork))
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

	case *protocol.RewindWorkflowRequest:
		var opts []dbos.RewindWorkflowOption
		if req.Body.StartStep != nil && *req.Body.StartStep > 0 {
			opts = append(opts, dbos.WithRewindStartStep(uint(*req.Body.StartStep)))
		}
		if req.Body.ApplicationVersion != nil {
			opts = append(opts, dbos.WithRewindApplicationVersion(*req.Body.ApplicationVersion))
		}
		if req.Body.QueueName != nil {
			opts = append(opts, dbos.WithRewindQueue(*req.Body.QueueName))
		}
		if req.Body.QueuePartitionKey != nil {
			opts = append(opts, dbos.WithRewindQueuePartitionKey(*req.Body.QueuePartitionKey))
		}
		if _, err := cli.RewindWorkflow(cli, req.Body.WorkflowID, opts...); err != nil {
			return nil, fmt.Errorf("failed to rewind workflow %s: %w", req.Body.WorkflowID, err)
		}
		return &protocol.RewindWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeRewindWorkflow,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil

	case *protocol.DeleteWorkflowRequest:
		ids := req.WorkflowIDs
		if len(ids) == 0 && req.WorkflowID != "" {
			ids = []string{req.WorkflowID}
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("missing workflow ID(s) in delete request")
		}
		var opts []dbos.DeleteWorkflowOption
		if req.DeleteChildren {
			opts = append(opts, dbos.WithDeleteChildren())
		}
		if err := cli.DeleteWorkflows(cli, ids, opts...); err != nil {
			return nil, fmt.Errorf("failed to delete workflows: %w", err)
		}
		return &protocol.DeleteWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeDelete,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil

	case *protocol.ExistPendingWorkflowsRequest:
		opts := []dbos.ListWorkflowsOption{
			dbos.WithFilterExecutorIDs(req.ExecutorID),
			dbos.WithFilterStatus(dbos.WorkflowStatusPending),
			dbos.WithFilterLimit(1),
		}
		if req.ApplicationVersion != "" {
			opts = append(opts, dbos.WithFilterAppVersion(req.ApplicationVersion))
		}
		statuses, err := cli.ListWorkflows(cli, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to check pending workflows for executor %s: %w", req.ExecutorID, err)
		}
		return &protocol.ExistPendingWorkflowsResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeExistPendingWorkflows,
				RequestID: req.RequestID,
			},
			Exist: len(statuses) > 0,
		}, nil

	case *protocol.ListSchedulesRequest:
		var opts []dbos.ListSchedulesOption
		if len(req.Body.Status) > 0 {
			statuses := make([]dbos.ScheduleStatus, 0, len(req.Body.Status))
			for _, s := range req.Body.Status {
				statuses = append(statuses, dbos.ScheduleStatus(s))
			}
			opts = append(opts, dbos.WithScheduleStatuses(statuses...))
		}
		if len(req.Body.WorkflowName) > 0 {
			opts = append(opts, dbos.WithScheduleWorkflowNames(req.Body.WorkflowName...))
		}
		if len(req.Body.ScheduleNamePrefix) > 0 {
			opts = append(opts, dbos.WithScheduleNamePrefixes(req.Body.ScheduleNamePrefix...))
		}
		if len(req.Body.ApplicationName) > 0 {
			opts = append(opts, dbos.WithScheduleApplicationNames(req.Body.ApplicationName...))
		}
		schedules, err := cli.ListSchedules(cli, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to list schedules: %w", err)
		}
		loadContext := req.Body.LoadContext == nil || *req.Body.LoadContext
		outputs := make([]protocol.ScheduleOutput, len(schedules))
		for i, s := range schedules {
			outputs[i] = mapSchedule(s, loadContext)
		}
		return &protocol.ListSchedulesResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListSchedules,
				RequestID: req.RequestID,
			},
			Output: outputs,
		}, nil

	case *protocol.GetScheduleRequest:
		schedule, err := cli.GetSchedule(cli, req.ScheduleName)
		if err != nil {
			return nil, fmt.Errorf("failed to get schedule %s: %w", req.ScheduleName, err)
		}
		loadContext := req.LoadContext == nil || *req.LoadContext
		output := mapSchedule(schedule, loadContext)
		return &protocol.GetScheduleResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetSchedule,
				RequestID: req.RequestID,
			},
			Output: &output,
		}, nil

	case *protocol.PauseScheduleRequest:
		if err := cli.PauseSchedule(cli, req.ScheduleName); err != nil {
			return nil, fmt.Errorf("failed to pause schedule %s: %w", req.ScheduleName, err)
		}
		return &protocol.PauseScheduleResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypePauseSchedule,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil

	case *protocol.ResumeScheduleRequest:
		if err := cli.ResumeSchedule(cli, req.ScheduleName); err != nil {
			return nil, fmt.Errorf("failed to resume schedule %s: %w", req.ScheduleName, err)
		}
		return &protocol.ResumeScheduleResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeResumeSchedule,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil

	case *protocol.BackfillScheduleRequest:
		start, err := time.Parse(time.RFC3339, req.Start)
		if err != nil {
			return nil, fmt.Errorf("invalid backfill start time %q: %w", req.Start, err)
		}
		end, err := time.Parse(time.RFC3339, req.End)
		if err != nil {
			return nil, fmt.Errorf("invalid backfill end time %q: %w", req.End, err)
		}
		ids, err := cli.BackfillSchedule(cli, req.ScheduleName, start, end)
		if err != nil {
			return nil, fmt.Errorf("failed to backfill schedule %s: %w", req.ScheduleName, err)
		}
		return &protocol.BackfillScheduleResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeBackfillSchedule,
				RequestID: req.RequestID,
			},
			WorkflowIDs: ids,
		}, nil

	case *protocol.TriggerScheduleRequest:
		handle, err := cli.TriggerSchedule(cli, req.ScheduleName)
		if err != nil {
			return nil, fmt.Errorf("failed to trigger schedule %s: %w", req.ScheduleName, err)
		}
		id := handle.GetWorkflowID()
		return &protocol.TriggerScheduleResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeTriggerSchedule,
				RequestID: req.RequestID,
			},
			WorkflowID: &id,
		}, nil

	case *protocol.ListQueuesRequest:
		var opts []dbos.ListQueuesOption
		if len(req.Body.ApplicationName) > 0 {
			opts = append(opts, dbos.WithListQueuesApplicationNames(req.Body.ApplicationName...))
		}
		queues, err := cli.ListQueues(cli, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to list queues: %w", err)
		}
		outputs := make([]protocol.QueueOutput, len(queues))
		for i, q := range queues {
			outputs[i] = mapQueue(q)
		}
		return &protocol.ListQueuesResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListQueues,
				RequestID: req.RequestID,
			},
			Output: outputs,
		}, nil

	case *protocol.GetQueueRequest:
		queue, err := cli.RetrieveQueue(cli, req.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get queue %s: %w", req.Name, err)
		}
		output := mapQueue(queue)
		return &protocol.GetQueueResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetQueue,
				RequestID: req.RequestID,
			},
			Output: &output,
		}, nil

	case *protocol.ListApplicationVersionsRequest:
		versions, err := cli.ListApplicationVersions(cli)
		if err != nil {
			return nil, fmt.Errorf("failed to list application versions: %w", err)
		}
		outputs := make([]protocol.ApplicationVersionOutput, len(versions))
		for i, v := range versions {
			outputs[i] = protocol.ApplicationVersionOutput{
				ID:        v.ID,
				Name:      v.Name,
				Timestamp: v.Timestamp,
				CreatedAt: v.CreatedAt,
			}
		}
		return &protocol.ListApplicationVersionsResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListApplicationVersions,
				RequestID: req.RequestID,
			},
			Output: outputs,
		}, nil

	case *protocol.SetLatestApplicationVersionRequest:
		if err := cli.SetLatestApplicationVersion(cli, req.VersionName); err != nil {
			return nil, fmt.Errorf("failed to set latest application version %s: %w", req.VersionName, err)
		}
		return &protocol.SetLatestApplicationVersionResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeSetLatestApplicationVersion,
				RequestID: req.RequestID,
			},
			Success: true,
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
			IsFork:                    req.Body.IsFork,
			Attributes:                req.Body.Attributes,
		}
		rows, err := cli.GetWorkflowAggregates(cli, input)
		if err != nil {
			return nil, fmt.Errorf("failed to get workflow aggregates: %w", err)
		}
		outputs := make([]protocol.WorkflowAggregateRow, len(rows))
		for i, row := range rows {
			outputs[i] = protocol.WorkflowAggregateRow{
				Group:             row.Group,
				Count:             row.Count,
				MinCreatedAt:      row.MinCreatedAt,
				MaxQueueWaitMs:    row.MaxQueueWaitMs,
				MaxTotalLatencyMs: row.MaxTotalLatencyMs,
			}
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
		outputs := make([]protocol.StepAggregateRow, len(rows))
		for i, row := range rows {
			outputs[i] = protocol.StepAggregateRow{
				Group:         row.Group,
				Count:         row.Count,
				MaxDurationMs: row.MaxDurationMs,
			}
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

// internalQueueName is the SDK's internal recovery queue. Source:
// dbos-transact-golang v1.7.0, dbos/internal/models (InternalQueueName).
const internalQueueName = "_dbos_internal_queue"

// Recover re-enqueues the pending workflows owned by the given executors.
// It uses the SDK's own system database operation, so Relay never writes the
// recovery SQL itself.
func (c *SDKClient) Recover(ctx context.Context, executorIDs []string, appVersion string) ([]string, error) {
	if len(executorIDs) == 0 {
		return nil, nil
	}
	dbosCtx, ok := c.client.(dbos.Context)
	if !ok {
		return nil, fmt.Errorf("data-plane client does not expose a DBOS context")
	}
	runtime, err := internals.RuntimeOf(dbosCtx)
	if err != nil {
		return nil, fmt.Errorf("resolving DBOS runtime for recovery: %w", err)
	}
	return runtime.SystemDB().ReenqueueForRecovery(ctx, executorIDs, appVersion, internalQueueName)
}

// HasPendingWorkflows reports whether the executor still owns PENDING
// workflows. It is the direct-mode equivalent of the socket
// exist_pending_workflows probe.
func (c *SDKClient) HasPendingWorkflows(ctx context.Context, executorID, appVersion string) (bool, error) {
	dbosCtx, ok := c.client.(dbos.Context)
	if !ok {
		return false, fmt.Errorf("data-plane client does not expose a DBOS context")
	}
	cli := dbos.From(dbosCtx, ctx)
	opts := []dbos.ListWorkflowsOption{
		dbos.WithFilterExecutorIDs(executorID),
		dbos.WithFilterStatus(dbos.WorkflowStatusPending),
		dbos.WithFilterLimit(1),
	}
	if appVersion != "" {
		opts = append(opts, dbos.WithFilterAppVersion(appVersion))
	}
	statuses, err := cli.ListWorkflows(cli, opts...)
	if err != nil {
		return false, err
	}
	return len(statuses) > 0, nil
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
	if s.Status == dbos.WorkflowStatusPending && !s.StartedAt.IsZero() {
		str := strconv.FormatInt(s.StartedAt.UnixMilli(), 10)
		body.DequeuedAt = &str
	}
	if s.DeduplicationID != "" {
		body.DeduplicationID = &s.DeduplicationID
	}
	priorityStr := strconv.Itoa(s.Priority)
	body.Priority = &priorityStr
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

func mapSchedule(s dbos.WorkflowSchedule, loadContext bool) protocol.ScheduleOutput {
	out := protocol.ScheduleOutput{
		ScheduleID:        s.ScheduleID,
		ScheduleName:      s.ScheduleName,
		WorkflowName:      s.WorkflowName,
		Schedule:          s.Schedule,
		Status:            string(s.Status),
		AutomaticBackfill: s.AutomaticBackfill,
	}
	if s.WorkflowClassName != "" {
		out.WorkflowClassName = &s.WorkflowClassName
	}
	if s.CronTimezone != "" {
		out.CronTimezone = &s.CronTimezone
	}
	if s.QueueName != "" {
		out.QueueName = &s.QueueName
	}
	if s.ApplicationName != "" {
		out.ApplicationName = &s.ApplicationName
	}
	if s.LastFiredAt != nil {
		str := s.LastFiredAt.UTC().Format(time.RFC3339Nano)
		out.LastFiredAt = &str
	}
	if loadContext && len(s.Context) > 0 {
		str := string(s.Context)
		out.Context = &str
	}
	return out
}

func mapQueue(q dbos.Queue) protocol.QueueOutput {
	cfg := q.Config()
	out := protocol.QueueOutput{
		Name:               cfg.Name,
		PriorityEnabled:    cfg.PriorityEnabled,
		PartitionQueue:     cfg.PartitionQueue,
		PollingIntervalSec: q.GetPollingInterval().Seconds(),
	}
	if cfg.ApplicationName != "" {
		out.ApplicationName = &cfg.ApplicationName
	}
	if cfg.GlobalConcurrency != nil {
		out.Concurrency = cfg.GlobalConcurrency
	}
	if cfg.WorkerConcurrency != nil {
		out.WorkerConcurrency = cfg.WorkerConcurrency
	}
	if cfg.RateLimit != nil {
		limit := cfg.RateLimit.Limit
		period := cfg.RateLimit.Period.Seconds()
		out.RateLimitMax = &limit
		out.RateLimitPeriodSec = &period
	}
	if cfg.PartitionConcurrency != nil {
		out.PartitionConcurrency = cfg.PartitionConcurrency
	}
	if cfg.PartitionWorkerConcurrency != nil {
		out.PartitionWorkerConcurrency = cfg.PartitionWorkerConcurrency
	}
	if cfg.PartitionRateLimit != nil {
		limit := cfg.PartitionRateLimit.Limit
		period := cfg.PartitionRateLimit.Period.Seconds()
		out.PartitionRateLimitMax = &limit
		out.PartitionRateLimitPeriodSec = &period
	}
	return out
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
