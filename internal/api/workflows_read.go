package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
)

func generateRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func handleEnvelopeError(errMsg *string) (int, gen.ErrorModel) {
	msg := *errMsg
	status := http.StatusBadRequest
	title := "Executor Error"
	if strings.Contains(strings.ToLower(msg), "not found") {
		status = http.StatusNotFound
		title = "Not Found"
	}
	return status, MakeErrorModel(status, title, msg)
}

func parseTimeString(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	str := strings.TrimSpace(*s)
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999-07",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, str); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	if n, err := strconv.ParseInt(str, 10, 64); err == nil {
		var t time.Time
		if n > 100000000000 {
			t = time.UnixMilli(n).UTC()
		} else {
			t = time.Unix(n, 0).UTC()
		}
		return &t
	}
	return nil
}


func mapWorkflow(b protocol.ListWorkflowsResponseBody) gen.Workflow {
	var status string
	if b.Status != nil {
		status = *b.Status
	}

	var createdAt time.Time
	if t := parseTimeString(b.CreatedAt); t != nil {
		createdAt = *t
	}

	var updatedAt time.Time
	if t := parseTimeString(b.UpdatedAt); t != nil {
		updatedAt = *t
	}

	var priority int32
	if b.Priority != nil {
		if v, err := strconv.ParseInt(*b.Priority, 10, 32); err == nil {
			priority = int32(v)
		}
	}

	var timeoutMs *int64
	if b.WorkflowTimeoutMS != nil {
		if v, err := strconv.ParseInt(*b.WorkflowTimeoutMS, 10, 64); err == nil {
			timeoutMs = &v
		}
	}

	var wasForkedFrom bool
	if b.WasForkedFrom != nil {
		wasForkedFrom = *b.WasForkedFrom
	}

	return gen.Workflow{
		AppVersion:        b.ApplicationVersion,
		ApplicationName:   b.ApplicationName,
		AssumedRole:       b.AssumedRole,
		Attributes:        b.Attributes,
		CompletedAt:       parseTimeString(b.CompletedAt),
		CreatedAt:         createdAt,
		Deadline:          parseTimeString(b.WorkflowDeadlineEpochMS),
		DeduplicationId:   b.DeduplicationID,
		DelayUntil:        parseTimeString(b.DelayUntilEpochMS),
		DequeuedAt:        parseTimeString(b.DequeuedAt),
		Error:             b.Error,
		ExecutorId:        b.ExecutorID,
		ForkedFrom:        b.ForkedFrom,
		Input:             b.Input,
		Output:            b.Output,
		ParentWorkflowId:  b.ParentWorkflowID,
		Priority:          priority,
		QueueName:         b.QueueName,
		QueuePartitionKey: b.QueuePartitionKey,
		Roles:             b.AuthenticatedRoles,
		ScheduleName:      b.ScheduleName,
		Status:            status,
		TimeoutMs:         timeoutMs,
		UpdatedAt:         updatedAt,
		User:              b.AuthenticatedUser,
		WasForkedFrom:     wasForkedFrom,
		WorkflowClass:     b.WorkflowClassName,
		WorkflowConfig:    b.WorkflowConfigName,
		WorkflowId:        b.WorkflowUUID,
		WorkflowName:      b.WorkflowName,
	}
}

func mapStep(s protocol.WorkflowStepsResponseBody) gen.Step {
	return gen.Step{
		ChildWorkflowId: s.ChildWorkflowID,
		CompletedAt:     parseTimeString(s.CompletedAtEpochMs),
		Error:           s.Error,
		Output:          s.Output,
		StartedAt:       parseTimeString(s.StartedAtEpochMs),
		StepId:          int32(s.FunctionID),
		StepName:        s.FunctionName,
	}
}

func mapWorkflowAggregate(m protocol.WorkflowAggregateRow) gen.WorkflowAggregate {
	var agg gen.WorkflowAggregate
	agg.Group = m.Group
	agg.Count = m.Count
	agg.MaxQueueWaitMs = m.MaxQueueWaitMs
	agg.MaxTotalLatencyMs = m.MaxTotalLatencyMs
	if m.MinCreatedAt != nil {
		t := time.UnixMilli(*m.MinCreatedAt).UTC()
		agg.MinCreatedAt = &t
	}
	return agg
}

func mapStepAggregate(m protocol.StepAggregateRow) gen.StepAggregate {
	var agg gen.StepAggregate
	agg.Group = m.Group
	agg.Count = m.Count
	agg.MaxDurationMs = m.MaxDurationMs
	return agg
}

// ListWorkflows retrieves workflows matching query parameters.
func (s *Server) ListWorkflows(ctx context.Context, request gen.ListWorkflowsRequestObject) (gen.ListWorkflowsResponseObject, error) {
	var body protocol.ListWorkflowsRequestBody
	if request.Params.Status != nil {
		body.Status = protocol.StringOrList{*request.Params.Status}
	}
	if request.Params.WorkflowName != nil {
		body.WorkflowName = protocol.StringOrList{*request.Params.WorkflowName}
	}
	if request.Params.Limit != nil {
		lim := int(*request.Params.Limit)
		body.Limit = &lim
	}
	if request.Params.Offset != nil {
		off := int(*request.Params.Offset)
		body.Offset = &off
	}
	if request.Params.SortDesc != nil {
		body.SortDesc = *request.Params.SortDesc
	}
	if request.Params.LoadInput != nil {
		body.LoadInput = *request.Params.LoadInput
	}
	if request.Params.LoadOutput != nil {
		body.LoadOutput = *request.Params.LoadOutput
	}

	reqMsg := &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListWorkflows,
			RequestID: generateRequestID(),
		},
		Body: body,
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ListWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.ListWorkflowsResponse)
	if !ok {
		return gen.ListWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.ListWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	workflows := make([]gen.Workflow, 0, len(resp.Output))
	for _, item := range resp.Output {
		workflows = append(workflows, mapWorkflow(item))
	}

	return gen.ListWorkflows200JSONResponse(workflows), nil
}

// SearchWorkflows performs advanced filtering over workflows.
func (s *Server) SearchWorkflows(ctx context.Context, request gen.SearchWorkflowsRequestObject) (gen.SearchWorkflowsResponseObject, error) {
	var body protocol.ListWorkflowsRequestBody
	if b := request.Body; b != nil {
		if b.WorkflowIds != nil {
			body.WorkflowUUIDs = *b.WorkflowIds
		}
		if b.WorkflowName != nil {
			body.WorkflowName = protocol.StringOrList(*b.WorkflowName)
		}
		if b.User != nil {
			body.AuthenticatedUser = protocol.StringOrList(*b.User)
		}
		body.StartTime = b.StartTime
		body.EndTime = b.EndTime
		body.CompletedAfter = b.CompletedAfter
		body.CompletedBefore = b.CompletedBefore
		body.DequeuedAfter = b.DequeuedAfter
		body.DequeuedBefore = b.DequeuedBefore
		if b.Status != nil {
			body.Status = protocol.StringOrList(*b.Status)
		}
		if b.AppVersion != nil {
			body.ApplicationVersion = protocol.StringOrList(*b.AppVersion)
		}
		if b.ForkedFrom != nil {
			body.ForkedFrom = protocol.StringOrList(*b.ForkedFrom)
		}
		if b.ParentWorkflowId != nil {
			body.ParentWorkflowID = protocol.StringOrList(*b.ParentWorkflowId)
		}
		body.WasForkedFrom = b.WasForkedFrom
		body.HasParent = b.HasParent
		if b.QueueName != nil {
			body.QueueName = protocol.StringOrList(*b.QueueName)
		}
		if b.Limit != nil {
			lim := int(*b.Limit)
			body.Limit = &lim
		}
		if b.Offset != nil {
			off := int(*b.Offset)
			body.Offset = &off
		}
		if b.SortDesc != nil {
			body.SortDesc = *b.SortDesc
		}
		if b.WorkflowIdPrefix != nil {
			body.WorkflowIDPrefix = protocol.StringOrList(*b.WorkflowIdPrefix)
		}
		if b.LoadInput != nil {
			body.LoadInput = *b.LoadInput
		}
		if b.LoadOutput != nil {
			body.LoadOutput = *b.LoadOutput
		}
		if b.ExecutorId != nil {
			body.ExecutorID = protocol.StringOrList(*b.ExecutorId)
		}
		if b.QueuesOnly != nil {
			body.QueuesOnly = *b.QueuesOnly
		}
		if b.Attributes != nil {
			body.Attributes = *b.Attributes
		}
		if b.ScheduleName != nil {
			body.ScheduleName = protocol.StringOrList(*b.ScheduleName)
		}
	}

	reqMsg := &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListWorkflows,
			RequestID: generateRequestID(),
		},
		Body: body,
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.SearchWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.ListWorkflowsResponse)
	if !ok {
		return gen.SearchWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.SearchWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	workflows := make([]gen.Workflow, 0, len(resp.Output))
	for _, item := range resp.Output {
		workflows = append(workflows, mapWorkflow(item))
	}

	return gen.SearchWorkflows200JSONResponse(workflows), nil
}

// GetWorkflow retrieves details of a specific workflow.
func (s *Server) GetWorkflow(ctx context.Context, request gen.GetWorkflowRequestObject) (gen.GetWorkflowResponseObject, error) {
	reqMsg := &protocol.GetWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflow,
			RequestID: generateRequestID(),
		},
		WorkflowID: request.WorkflowId,
		LoadInput:  true,
		LoadOutput: true,
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.GetWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.GetWorkflowResponse)
	if !ok {
		return gen.GetWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.GetWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	if resp.Output == nil {
		return gen.GetWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Workflow not found", fmt.Sprintf("workflow %q not found", request.WorkflowId)),
		}, nil
	}

	return gen.GetWorkflow200JSONResponse(mapWorkflow(*resp.Output)), nil
}

// ListWorkflowSteps retrieves execution steps for a workflow.
func (s *Server) ListWorkflowSteps(ctx context.Context, request gen.ListWorkflowStepsRequestObject) (gen.ListWorkflowStepsResponseObject, error) {
	reqMsg := &protocol.ListStepsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListSteps,
			RequestID: generateRequestID(),
		},
		WorkflowID: request.WorkflowId,
		LoadOutput: true,
	}
	if request.Params.Limit != nil {
		lim := int(*request.Params.Limit)
		reqMsg.Limit = &lim
	}
	if request.Params.Offset != nil {
		off := int(*request.Params.Offset)
		reqMsg.Offset = &off
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ListWorkflowStepsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.ListStepsResponse)
	if !ok {
		return gen.ListWorkflowStepsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.ListWorkflowStepsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	steps := make([]gen.Step, 0)
	if resp.Output != nil {
		for _, item := range *resp.Output {
			steps = append(steps, mapStep(item))
		}
	}

	return gen.ListWorkflowSteps200JSONResponse(steps), nil
}

// ListWorkflowEvents retrieves recorded events for a workflow.
func (s *Server) ListWorkflowEvents(ctx context.Context, request gen.ListWorkflowEventsRequestObject) (gen.ListWorkflowEventsResponseObject, error) {
	reqMsg := &protocol.GetWorkflowEventsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflowEvents,
			RequestID: generateRequestID(),
		},
		WorkflowID: request.WorkflowId,
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ListWorkflowEventsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.GetWorkflowEventsResponse)
	if !ok {
		return gen.ListWorkflowEventsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.ListWorkflowEventsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	events := make([]gen.Event, 0, len(resp.Events))
	for _, e := range resp.Events {
		events = append(events, gen.Event{
			Key:   e.Key,
			Value: e.Value,
		})
	}

	return gen.ListWorkflowEvents200JSONResponse(events), nil
}

// ListWorkflowNotifications retrieves notifications sent to a workflow.
func (s *Server) ListWorkflowNotifications(ctx context.Context, request gen.ListWorkflowNotificationsRequestObject) (gen.ListWorkflowNotificationsResponseObject, error) {
	reqMsg := &protocol.GetWorkflowNotificationsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflowNotifications,
			RequestID: generateRequestID(),
		},
		WorkflowID: request.WorkflowId,
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ListWorkflowNotificationsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.GetWorkflowNotificationsResponse)
	if !ok {
		return gen.ListWorkflowNotificationsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.ListWorkflowNotificationsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	notifs := make([]gen.Notification, 0, len(resp.Notifications))
	for _, n := range resp.Notifications {
		notifs = append(notifs, gen.Notification{
			Topic:     n.Topic,
			Message:   n.Message,
			CreatedAt: time.UnixMilli(n.CreatedAtEpochMs).UTC(),
			Consumed:  n.Consumed,
		})
	}

	return gen.ListWorkflowNotifications200JSONResponse(notifs), nil
}

// ListWorkflowStreams retrieves stream entries for a workflow.
func (s *Server) ListWorkflowStreams(ctx context.Context, request gen.ListWorkflowStreamsRequestObject) (gen.ListWorkflowStreamsResponseObject, error) {
	reqMsg := &protocol.GetWorkflowStreamsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflowStreams,
			RequestID: generateRequestID(),
		},
		WorkflowID: request.WorkflowId,
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ListWorkflowStreamsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.GetWorkflowStreamsResponse)
	if !ok {
		return gen.ListWorkflowStreamsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.ListWorkflowStreamsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	streams := make([]gen.StreamEntry, 0, len(resp.Streams))
	for _, st := range resp.Streams {
		streams = append(streams, gen.StreamEntry{
			Key:    st.Key,
			Values: st.Values,
		})
	}

	return gen.ListWorkflowStreams200JSONResponse(streams), nil
}

// ExportWorkflow serializes a workflow for export.
func (s *Server) ExportWorkflow(ctx context.Context, request gen.ExportWorkflowRequestObject) (gen.ExportWorkflowResponseObject, error) {
	reqMsg := &protocol.ExportWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExportWorkflow,
			RequestID: generateRequestID(),
		},
		WorkflowID: request.WorkflowId,
	}
	if request.Params.ExportChildren != nil {
		reqMsg.ExportChildren = *request.Params.ExportChildren
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ExportWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.ExportWorkflowResponse)
	if !ok {
		return gen.ExportWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.ExportWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	var serialized string
	if resp.SerializedWorkflow != nil {
		serialized = *resp.SerializedWorkflow
	}

	return gen.ExportWorkflow200JSONResponse(gen.ExportWorkflowOutputBody{
		SerializedWorkflow: serialized,
	}), nil
}

// GetWorkflowAggregates calculates aggregates over workflows matching criteria.
func (s *Server) GetWorkflowAggregates(ctx context.Context, request gen.GetWorkflowAggregatesRequestObject) (gen.GetWorkflowAggregatesResponseObject, error) {
	var body protocol.GetWorkflowAggregatesRequestBody
	if b := request.Body; b != nil {
		if b.GroupByStatus != nil {
			body.GroupByStatus = *b.GroupByStatus
		}
		if b.GroupByWorkflowName != nil {
			body.GroupByName = *b.GroupByWorkflowName
		}
		if b.GroupByQueueName != nil {
			body.GroupByQueueName = *b.GroupByQueueName
		}
		if b.GroupByExecutorId != nil {
			body.GroupByExecutorID = *b.GroupByExecutorId
		}
		if b.GroupByAppVersion != nil {
			body.GroupByApplicationVersion = *b.GroupByAppVersion
		}
		if b.GroupByApplicationName != nil {
			body.GroupByApplicationName = *b.GroupByApplicationName
		}
		if b.SelectCount != nil {
			body.SelectCount = *b.SelectCount
		}
		if b.SelectMinCreatedAt != nil {
			body.SelectMinCreatedAt = *b.SelectMinCreatedAt
		}
		if b.SelectMaxQueueWaitMs != nil {
			body.SelectMaxQueueWaitMs = *b.SelectMaxQueueWaitMs
		}
		if b.SelectMaxTotalLatencyMs != nil {
			body.SelectMaxTotalLatencyMs = *b.SelectMaxTotalLatencyMs
		}
		body.TimeBucketSizeMs = b.TimeBucketSizeMs
		if b.Status != nil {
			body.Status = protocol.StringOrList(*b.Status)
		}
		body.StartTime = b.StartTime
		body.EndTime = b.EndTime
		body.CompletedAfter = b.CompletedAfter
		body.CompletedBefore = b.CompletedBefore
		body.DequeuedAfter = b.DequeuedAfter
		body.DequeuedBefore = b.DequeuedBefore
		if b.WorkflowName != nil {
			body.Name = protocol.StringOrList(*b.WorkflowName)
		}
		if b.AppVersion != nil {
			body.AppVersion = protocol.StringOrList(*b.AppVersion)
		}
		if b.ExecutorId != nil {
			body.ExecutorID = protocol.StringOrList(*b.ExecutorId)
		}
		if b.QueueName != nil {
			body.QueueName = protocol.StringOrList(*b.QueueName)
		}
		if b.WorkflowIdPrefix != nil {
			body.WorkflowIDPrefix = protocol.StringOrList(*b.WorkflowIdPrefix)
		}
		if b.WorkflowIds != nil {
			body.WorkflowIDs = protocol.StringOrList(*b.WorkflowIds)
		}
		if b.ForkedFrom != nil {
			body.ForkedFrom = protocol.StringOrList(*b.ForkedFrom)
		}
		if b.ParentWorkflowId != nil {
			body.ParentWorkflowID = protocol.StringOrList(*b.ParentWorkflowId)
		}
		if b.User != nil {
			body.User = protocol.StringOrList(*b.User)
		}
		body.WasForkedFrom = b.WasForkedFrom
		body.HasParent = b.HasParent
		if b.Attributes != nil {
			body.Attributes = *b.Attributes
		}
	}

	reqMsg := &protocol.GetWorkflowAggregatesRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflowAggregates,
			RequestID: generateRequestID(),
		},
		Body: body,
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.GetWorkflowAggregatesdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.GetWorkflowAggregatesResponse)
	if !ok {
		return gen.GetWorkflowAggregatesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.GetWorkflowAggregatesdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	aggregates := make([]gen.WorkflowAggregate, 0, len(resp.Output))
	for _, m := range resp.Output {
		aggregates = append(aggregates, mapWorkflowAggregate(m))
	}

	return gen.GetWorkflowAggregates200JSONResponse(aggregates), nil
}

// GetStepAggregates calculates aggregates over workflow steps matching criteria.
func (s *Server) GetStepAggregates(ctx context.Context, request gen.GetStepAggregatesRequestObject) (gen.GetStepAggregatesResponseObject, error) {
	var body protocol.GetStepAggregatesRequestBody
	if b := request.Body; b != nil {
		if b.GroupByFunctionName != nil {
			body.GroupByFunctionName = *b.GroupByFunctionName
		}
		if b.GroupByStatus != nil {
			body.GroupByStatus = *b.GroupByStatus
		}
		if b.SelectCount != nil {
			body.SelectCount = *b.SelectCount
		}
		if b.SelectMaxDurationMs != nil {
			body.SelectMaxDurationMs = *b.SelectMaxDurationMs
		}
		body.TimeBucketSizeMs = b.TimeBucketSizeMs
		if b.Status != nil {
			body.Status = protocol.StringOrList(*b.Status)
		}
		if b.StepName != nil {
			body.FunctionName = protocol.StringOrList(*b.StepName)
		}
		if b.WorkflowIdPrefix != nil {
			body.WorkflowIDPrefix = protocol.StringOrList(*b.WorkflowIdPrefix)
		}
		body.CompletedAfter = b.CompletedAfter
		body.CompletedBefore = b.CompletedBefore
	}

	reqMsg := &protocol.GetStepAggregatesRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetStepAggregates,
			RequestID: generateRequestID(),
		},
		Body: body,
	}

	res, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, reqMsg)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.GetStepAggregatesdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	resp, ok := res.(*protocol.GetStepAggregatesResponse)
	if !ok {
		return gen.GetStepAggregatesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.ErrorMessage != nil {
		status, errModel := handleEnvelopeError(resp.ErrorMessage)
		return gen.GetStepAggregatesdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: errModel}, nil
	}

	aggregates := make([]gen.StepAggregate, 0, len(resp.Output))
	for _, m := range resp.Output {
		aggregates = append(aggregates, mapStepAggregate(m))
	}

	return gen.GetStepAggregates200JSONResponse(aggregates), nil
}
