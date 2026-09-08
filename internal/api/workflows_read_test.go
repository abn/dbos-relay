package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
)

type mockWorkflowRouter struct {
	dispatchFunc func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error)
}

func (m *mockWorkflowRouter) Dispatch(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
	if m.dispatchFunc != nil {
		return m.dispatchFunc(ctx, orgName, appName, msg)
	}
	return nil, nil
}

func TestListWorkflows(t *testing.T) {
	t.Run("successful dispatch and model translation", func(t *testing.T) {
		status := "SUCCESS"
		wfName := "order-process"
		user := "alice"
		appVer := "v1.2.0"
		createdAtStr := "2026-09-01T10:00:00Z"
		updatedAtStr := "2026-09-01T10:05:00Z"
		completedAtStr := "2026-09-01T10:04:30Z"
		deadlineStr := "1788257070000"
		priorityStr := "5"
		timeoutStr := "30000"
		wasForked := true
		input := `{"order_id":123}`
		output := `{"receipt":"rec-456"}`

		var capturedMsg protocol.Message
		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				if orgName != "my-org" || appName != "my-app" {
					t.Errorf("unexpected org/app: %s/%s", orgName, appName)
				}
				capturedMsg = msg
				return &protocol.ListWorkflowsResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeListWorkflows,
						RequestID: msg.GetRequestID(),
					},
					Output: []protocol.ListWorkflowsResponseBody{
						{
							WorkflowUUID:            "wf-12345",
							Status:                  &status,
							WorkflowName:            &wfName,
							AuthenticatedUser:       &user,
							ApplicationVersion:      &appVer,
							CreatedAt:               &createdAtStr,
							UpdatedAt:               &updatedAtStr,
							CompletedAt:             &completedAtStr,
							WorkflowDeadlineEpochMS: &deadlineStr,
							Priority:                &priorityStr,
							WorkflowTimeoutMS:       &timeoutStr,
							WasForkedFrom:           &wasForked,
							Input:                   &input,
							Output:                  &output,
						},
					},
				}, nil
			},
		}

		server := api.NewServer(r, nil, nil)
		limit := int64(20)
		offset := int64(10)
		sortDesc := true
		loadInput := true
		loadOutput := true

		respObj, err := server.ListWorkflows(context.Background(), gen.ListWorkflowsRequestObject{
			OrgName: "my-org",
			AppName: "my-app",
			Params: gen.ListWorkflowsParams{
				Status:       &status,
				WorkflowName: &wfName,
				Limit:        &limit,
				Offset:       &offset,
				SortDesc:     &sortDesc,
				LoadInput:    &loadInput,
				LoadOutput:   &loadOutput,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req, ok := capturedMsg.(*protocol.ListWorkflowsRequest)
		if !ok {
			t.Fatalf("expected *protocol.ListWorkflowsRequest, got %T", capturedMsg)
		}
		if req.Type != protocol.MessageTypeListWorkflows {
			t.Errorf("expected type %s, got %s", protocol.MessageTypeListWorkflows, req.Type)
		}
		if req.RequestID == "" {
			t.Error("expected non-empty RequestID")
		}
		if req.Body.Limit == nil || *req.Body.Limit != 20 {
			t.Errorf("expected limit 20, got %v", req.Body.Limit)
		}
		if req.Body.Offset == nil || *req.Body.Offset != 10 {
			t.Errorf("expected offset 10, got %v", req.Body.Offset)
		}
		if !req.Body.SortDesc {
			t.Error("expected SortDesc true")
		}
		if !req.Body.LoadInput {
			t.Error("expected LoadInput true")
		}
		if !req.Body.LoadOutput {
			t.Error("expected LoadOutput true")
		}

		resp200, ok := respObj.(gen.ListWorkflows200JSONResponse)
		if !ok {
			t.Fatalf("expected gen.ListWorkflows200JSONResponse, got %T", respObj)
		}
		if len(resp200) != 1 {
			t.Fatalf("expected 1 workflow, got %d", len(resp200))
		}
		wf := resp200[0]
		if wf.WorkflowId != "wf-12345" {
			t.Errorf("expected WorkflowId wf-12345, got %s", wf.WorkflowId)
		}
		if wf.Status != status {
			t.Errorf("expected Status %s, got %s", status, wf.Status)
		}
		if wf.WorkflowName == nil || *wf.WorkflowName != wfName {
			t.Errorf("expected WorkflowName %s, got %v", wfName, wf.WorkflowName)
		}
		if wf.Priority != 5 {
			t.Errorf("expected Priority 5, got %d", wf.Priority)
		}
		if wf.TimeoutMs == nil || *wf.TimeoutMs != 30000 {
			t.Errorf("expected TimeoutMs 30000, got %v", wf.TimeoutMs)
		}
		if !wf.WasForkedFrom {
			t.Error("expected WasForkedFrom true")
		}
		if wf.CreatedAt.IsZero() {
			t.Error("expected valid CreatedAt")
		}
		if wf.CompletedAt == nil || wf.CompletedAt.IsZero() {
			t.Error("expected valid CompletedAt")
		}
		if wf.Deadline == nil || wf.Deadline.IsZero() {
			t.Error("expected valid Deadline")
		}
	})

	t.Run("router error mapping", func(t *testing.T) {
		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, _ protocol.Message) (protocol.Message, error) {
				return nil, router.ErrOrgNotFound
			},
		}

		server := api.NewServer(r, nil, nil)
		respObj, err := server.ListWorkflows(context.Background(), gen.ListWorkflowsRequestObject{
			OrgName: "unknown-org",
			AppName: "my-app",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		prob, ok := respObj.(gen.ListWorkflowsdefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", respObj)
		}
		if prob.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", prob.StatusCode)
		}
	})

	t.Run("envelope error handling", func(t *testing.T) {
		errMsg := "execution failed: database query failed"
		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
				return &protocol.ListWorkflowsResponse{
					Envelope: protocol.Envelope{
						Type:         protocol.MessageTypeListWorkflows,
						RequestID:    msg.GetRequestID(),
						ErrorMessage: &errMsg,
					},
				}, nil
			},
		}

		server := api.NewServer(r, nil, nil)
		respObj, err := server.ListWorkflows(context.Background(), gen.ListWorkflowsRequestObject{
			OrgName: "my-org",
			AppName: "my-app",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		prob, ok := respObj.(gen.ListWorkflowsdefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected problem response, got %T", respObj)
		}
		if prob.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", prob.StatusCode)
		}
	})
}

func TestSearchWorkflows(t *testing.T) {
	t.Run("maps body filters to protocol message", func(t *testing.T) {
		var capturedMsg protocol.Message
		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
				capturedMsg = msg
				return &protocol.ListWorkflowsResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeListWorkflows,
						RequestID: msg.GetRequestID(),
					},
					Output: []protocol.ListWorkflowsResponseBody{},
				}, nil
			},
		}

		server := api.NewServer(r, nil, nil)
		limit := int64(15)
		offset := int64(5)
		sortDesc := true
		loadInput := true
		loadOutput := true
		wasForked := false
		hasParent := true
		queuesOnly := false
		now := time.Now().UTC()

		respObj, err := server.SearchWorkflows(context.Background(), gen.SearchWorkflowsRequestObject{
			OrgName: "my-org",
			AppName: "my-app",
			Body: &gen.WorkflowSearchBody{
				WorkflowIds:      &[]string{"wf-1", "wf-2"},
				WorkflowName:     &[]string{"test-wf"},
				User:             &[]string{"bob"},
				Status:           &[]string{"PENDING", "SUCCESS"},
				AppVersion:       &[]string{"v1.0.0"},
				ForkedFrom:       &[]string{"wf-parent"},
				ParentWorkflowId: &[]string{"wf-root"},
				QueueName:        &[]string{"default-queue"},
				WorkflowIdPrefix: &[]string{"wf-"},
				ExecutorId:       &[]string{"exec-1"},
				ScheduleName:     &[]string{"daily-cron"},
				Limit:            &limit,
				Offset:           &offset,
				SortDesc:         &sortDesc,
				LoadInput:        &loadInput,
				LoadOutput:       &loadOutput,
				WasForkedFrom:    &wasForked,
				HasParent:        &hasParent,
				QueuesOnly:       &queuesOnly,
				StartTime:        &now,
				EndTime:          &now,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		req, ok := capturedMsg.(*protocol.ListWorkflowsRequest)
		if !ok {
			t.Fatalf("expected *protocol.ListWorkflowsRequest, got %T", capturedMsg)
		}

		if len(req.Body.WorkflowUUIDs) != 2 || req.Body.WorkflowUUIDs[0] != "wf-1" {
			t.Errorf("unexpected WorkflowUUIDs: %v", req.Body.WorkflowUUIDs)
		}
		if len(req.Body.WorkflowName) != 1 || req.Body.WorkflowName[0] != "test-wf" {
			t.Errorf("unexpected WorkflowName: %v", req.Body.WorkflowName)
		}
		if len(req.Body.AuthenticatedUser) != 1 || req.Body.AuthenticatedUser[0] != "bob" {
			t.Errorf("unexpected AuthenticatedUser: %v", req.Body.AuthenticatedUser)
		}
		if len(req.Body.Status) != 2 {
			t.Errorf("unexpected Status: %v", req.Body.Status)
		}
		if len(req.Body.QueueName) != 1 || req.Body.QueueName[0] != "default-queue" {
			t.Errorf("unexpected QueueName: %v", req.Body.QueueName)
		}
		if req.Body.Limit == nil || *req.Body.Limit != 15 {
			t.Errorf("unexpected Limit: %v", req.Body.Limit)
		}
		if req.Body.Offset == nil || *req.Body.Offset != 5 {
			t.Errorf("unexpected Offset: %v", req.Body.Offset)
		}
		if !req.Body.SortDesc {
			t.Error("expected SortDesc true")
		}
		if !req.Body.LoadInput {
			t.Error("expected LoadInput true")
		}
		if !req.Body.LoadOutput {
			t.Error("expected LoadOutput true")
		}

		if _, ok := respObj.(gen.SearchWorkflows200JSONResponse); !ok {
			t.Fatalf("expected gen.SearchWorkflows200JSONResponse, got %T", respObj)
		}
	})
}

func TestGetWorkflow(t *testing.T) {
	t.Run("returns 200 with workflow details", func(t *testing.T) {
		status := "SUCCESS"
		wfName := "ingest-data"
		createdAtStr := "2026-09-02T12:00:00Z"
		updatedAtStr := "2026-09-02T12:01:00Z"

		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.GetWorkflowRequest)
				if !ok {
					t.Fatalf("expected *protocol.GetWorkflowRequest, got %T", msg)
				}
				if req.WorkflowID != "target-wf-id" {
					t.Errorf("expected workflow ID 'target-wf-id', got %s", req.WorkflowID)
				}
				if !req.LoadInput || !req.LoadOutput {
					t.Error("expected LoadInput and LoadOutput true")
				}
				return &protocol.GetWorkflowResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeGetWorkflow,
						RequestID: msg.GetRequestID(),
					},
					Output: &protocol.ListWorkflowsResponseBody{
						WorkflowUUID: "target-wf-id",
						Status:       &status,
						WorkflowName: &wfName,
						CreatedAt:    &createdAtStr,
						UpdatedAt:    &updatedAtStr,
					},
				}, nil
			},
		}

		server := api.NewServer(r, nil, nil)
		respObj, err := server.GetWorkflow(context.Background(), gen.GetWorkflowRequestObject{
			OrgName:    "my-org",
			AppName:    "my-app",
			WorkflowId: "target-wf-id",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resp200, ok := respObj.(gen.GetWorkflow200JSONResponse)
		if !ok {
			t.Fatalf("expected gen.GetWorkflow200JSONResponse, got %T", respObj)
		}
		if resp200.WorkflowId != "target-wf-id" {
			t.Errorf("expected WorkflowId target-wf-id, got %s", resp200.WorkflowId)
		}
		if resp200.Status != "SUCCESS" {
			t.Errorf("expected Status SUCCESS, got %s", resp200.Status)
		}
	})

	t.Run("returns 404 Problem on router.ErrAppNotFound", func(t *testing.T) {
		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, _ protocol.Message) (protocol.Message, error) {
				return nil, router.ErrAppNotFound
			},
		}

		server := api.NewServer(r, nil, nil)
		respObj, err := server.GetWorkflow(context.Background(), gen.GetWorkflowRequestObject{
			OrgName:    "my-org",
			AppName:    "missing-app",
			WorkflowId: "target-wf-id",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		prob, ok := respObj.(gen.GetWorkflowdefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected problem response, got %T", respObj)
		}
		if prob.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", prob.StatusCode)
		}
		if prob.Body.Title == nil || *prob.Body.Title != "Application not found" {
			t.Errorf("unexpected title: %v", prob.Body.Title)
		}
	})

	t.Run("returns 404 Problem when output is nil", func(t *testing.T) {
		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
				return &protocol.GetWorkflowResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeGetWorkflow,
						RequestID: msg.GetRequestID(),
					},
					Output: nil,
				}, nil
			},
		}

		server := api.NewServer(r, nil, nil)
		respObj, err := server.GetWorkflow(context.Background(), gen.GetWorkflowRequestObject{
			OrgName:    "my-org",
			AppName:    "my-app",
			WorkflowId: "missing-wf",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		prob, ok := respObj.(gen.GetWorkflowdefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected problem response, got %T", respObj)
		}
		if prob.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", prob.StatusCode)
		}
	})
}

func TestListWorkflowSteps(t *testing.T) {
	t.Run("returns step array", func(t *testing.T) {
		outputVal := `"step-output"`
		started := "1725200000000"
		completed := "1725200001000"
		childID := "child-wf-1"

		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.ListStepsRequest)
				if !ok {
					t.Fatalf("expected *protocol.ListStepsRequest, got %T", msg)
				}
				if req.WorkflowID != "wf-with-steps" {
					t.Errorf("expected WorkflowID wf-with-steps, got %s", req.WorkflowID)
				}
				return &protocol.ListStepsResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeListSteps,
						RequestID: msg.GetRequestID(),
					},
					Output: &[]protocol.WorkflowStepsResponseBody{
						{
							FunctionID:         1,
							FunctionName:       "stepOne",
							Output:             &outputVal,
							StartedAtEpochMs:   &started,
							CompletedAtEpochMs: &completed,
							ChildWorkflowID:    &childID,
						},
						{
							FunctionID:   2,
							FunctionName: "stepTwo",
						},
					},
				}, nil
			},
		}

		server := api.NewServer(r, nil, nil)
		limit := int64(10)
		respObj, err := server.ListWorkflowSteps(context.Background(), gen.ListWorkflowStepsRequestObject{
			OrgName:    "my-org",
			AppName:    "my-app",
			WorkflowId: "wf-with-steps",
			Params: gen.ListWorkflowStepsParams{
				Limit: &limit,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resp200, ok := respObj.(gen.ListWorkflowSteps200JSONResponse)
		if !ok {
			t.Fatalf("expected gen.ListWorkflowSteps200JSONResponse, got %T", respObj)
		}
		if len(resp200) != 2 {
			t.Fatalf("expected 2 steps, got %d", len(resp200))
		}
		s1 := resp200[0]
		if s1.StepId != 1 {
			t.Errorf("expected StepId 1, got %d", s1.StepId)
		}
		if s1.StepName != "stepOne" {
			t.Errorf("expected StepName stepOne, got %s", s1.StepName)
		}
		if s1.Output == nil || *s1.Output != outputVal {
			t.Errorf("expected output %s, got %v", outputVal, s1.Output)
		}
		if s1.ChildWorkflowId == nil || *s1.ChildWorkflowId != childID {
			t.Errorf("expected ChildWorkflowId %s, got %v", childID, s1.ChildWorkflowId)
		}
		if s1.StartedAt == nil || s1.StartedAt.IsZero() {
			t.Error("expected valid StartedAt")
		}
		if s1.CompletedAt == nil || s1.CompletedAt.IsZero() {
			t.Error("expected valid CompletedAt")
		}
	})
}

func TestRouterErrorToModel(t *testing.T) {
	t.Run("returns 503 Problem on router.ErrNoLiveExecutor", func(t *testing.T) {
		status, errModel := api.RouterErrorToModel(router.ErrNoLiveExecutor)
		if status != http.StatusServiceUnavailable {
			t.Errorf("expected status 503, got %d", status)
		}
		if errModel.Status == nil || *errModel.Status != int64(http.StatusServiceUnavailable) {
			t.Errorf("expected error model status 503, got %v", errModel.Status)
		}
		if errModel.Title == nil || *errModel.Title != "Service Unavailable" {
			t.Errorf("expected title 'Service Unavailable', got %v", errModel.Title)
		}
	})

	t.Run("returns 504 on router.ErrExecutorTimeout", func(t *testing.T) {
		status, _ := api.RouterErrorToModel(router.ErrExecutorTimeout)
		if status != http.StatusGatewayTimeout {
			t.Errorf("expected status 504, got %d", status)
		}
	})

	t.Run("returns 400 on router.ErrExecutorError", func(t *testing.T) {
		status, _ := api.RouterErrorToModel(router.ErrExecutorError)
		if status != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", status)
		}
	})

	t.Run("returns 500 on unexpected error", func(t *testing.T) {
		status, _ := api.RouterErrorToModel(errors.New("something broke"))
		if status != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", status)
		}
	})
}

func TestListWorkflowEvents(t *testing.T) {
	r := &mockWorkflowRouter{
		dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
			return &protocol.GetWorkflowEventsResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeGetWorkflowEvents,
					RequestID: msg.GetRequestID(),
				},
				Events: []protocol.EventOutput{
					{Key: "evt-key-1", Value: "evt-val-1"},
					{Key: "evt-key-2", Value: "evt-val-2"},
				},
			}, nil
		},
	}

	server := api.NewServer(r, nil, nil)
	respObj, err := server.ListWorkflowEvents(context.Background(), gen.ListWorkflowEventsRequestObject{
		OrgName:    "my-org",
		AppName:    "my-app",
		WorkflowId: "wf-events",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events, ok := respObj.(gen.ListWorkflowEvents200JSONResponse)
	if !ok {
		t.Fatalf("expected gen.ListWorkflowEvents200JSONResponse, got %T", respObj)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Key != "evt-key-1" || events[0].Value != "evt-val-1" {
		t.Errorf("unexpected event 0: %+v", events[0])
	}
}

func TestListWorkflowNotifications(t *testing.T) {
	topic := "alerts"
	r := &mockWorkflowRouter{
		dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
			return &protocol.GetWorkflowNotificationsResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeGetWorkflowNotifications,
					RequestID: msg.GetRequestID(),
				},
				Notifications: []protocol.NotificationOutput{
					{
						Topic:            &topic,
						Message:          "task complete",
						CreatedAtEpochMs: 1725200000000,
						Consumed:         true,
					},
				},
			}, nil
		},
	}

	server := api.NewServer(r, nil, nil)
	respObj, err := server.ListWorkflowNotifications(context.Background(), gen.ListWorkflowNotificationsRequestObject{
		OrgName:    "my-org",
		AppName:    "my-app",
		WorkflowId: "wf-notifs",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	notifs, ok := respObj.(gen.ListWorkflowNotifications200JSONResponse)
	if !ok {
		t.Fatalf("expected gen.ListWorkflowNotifications200JSONResponse, got %T", respObj)
	}
	if len(notifs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifs))
	}
	if notifs[0].Topic == nil || *notifs[0].Topic != "alerts" {
		t.Errorf("unexpected topic: %v", notifs[0].Topic)
	}
	if notifs[0].Message != "task complete" {
		t.Errorf("unexpected message: %s", notifs[0].Message)
	}
	if !notifs[0].Consumed {
		t.Error("expected consumed true")
	}
	if notifs[0].CreatedAt.IsZero() {
		t.Error("expected valid CreatedAt")
	}
}

func TestListWorkflowStreams(t *testing.T) {
	r := &mockWorkflowRouter{
		dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
			return &protocol.GetWorkflowStreamsResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeGetWorkflowStreams,
					RequestID: msg.GetRequestID(),
				},
				Streams: []protocol.StreamEntryOutput{
					{Key: "stream-1", Values: []string{"val1", "val2"}},
				},
			}, nil
		},
	}

	server := api.NewServer(r, nil, nil)
	respObj, err := server.ListWorkflowStreams(context.Background(), gen.ListWorkflowStreamsRequestObject{
		OrgName:    "my-org",
		AppName:    "my-app",
		WorkflowId: "wf-streams",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	streams, ok := respObj.(gen.ListWorkflowStreams200JSONResponse)
	if !ok {
		t.Fatalf("expected gen.ListWorkflowStreams200JSONResponse, got %T", respObj)
	}
	if len(streams) != 1 {
		t.Fatalf("expected 1 stream, got %d", len(streams))
	}
	if streams[0].Key != "stream-1" || len(streams[0].Values) != 2 {
		t.Errorf("unexpected stream entry: %+v", streams[0])
	}
}

func TestExportWorkflow(t *testing.T) {
	serialized := `{"workflow":"exported-data"}`
	r := &mockWorkflowRouter{
		dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
			req, ok := msg.(*protocol.ExportWorkflowRequest)
			if !ok {
				t.Fatalf("expected *protocol.ExportWorkflowRequest, got %T", msg)
			}
			if !req.ExportChildren {
				t.Error("expected ExportChildren true")
			}
			return &protocol.ExportWorkflowResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeExportWorkflow,
					RequestID: msg.GetRequestID(),
				},
				SerializedWorkflow: &serialized,
			}, nil
		},
	}

	exportChildren := true
	server := api.NewServer(r, nil, nil)
	respObj, err := server.ExportWorkflow(context.Background(), gen.ExportWorkflowRequestObject{
		OrgName:    "my-org",
		AppName:    "my-app",
		WorkflowId: "wf-export",
		Params: gen.ExportWorkflowParams{
			ExportChildren: &exportChildren,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	exportResp, ok := respObj.(gen.ExportWorkflow200JSONResponse)
	if !ok {
		t.Fatalf("expected gen.ExportWorkflow200JSONResponse, got %T", respObj)
	}
	if exportResp.SerializedWorkflow != serialized {
		t.Errorf("expected serialized workflow %s, got %s", serialized, exportResp.SerializedWorkflow)
	}
}

func TestGetWorkflowAggregates(t *testing.T) {
	r := &mockWorkflowRouter{
		dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
			req, ok := msg.(*protocol.GetWorkflowAggregatesRequest)
			if !ok {
				t.Fatalf("expected *protocol.GetWorkflowAggregatesRequest, got %T", msg)
			}
			if !req.Body.GroupByStatus {
				t.Error("expected GroupByStatus true")
			}
			return &protocol.GetWorkflowAggregatesResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeGetWorkflowAggregates,
					RequestID: msg.GetRequestID(),
				},
				Output: []map[string]any{
					{
						"count":                 int64(42),
						"status":                "SUCCESS",
						"max_queue_wait_ms":     int64(150),
						"max_total_latency_ms":  int64(500),
						"min_created_at":        "2026-09-01T08:00:00Z",
					},
				},
			}, nil
		},
	}

	groupByStatus := true
	selectCount := true
	server := api.NewServer(r, nil, nil)
	respObj, err := server.GetWorkflowAggregates(context.Background(), gen.GetWorkflowAggregatesRequestObject{
		OrgName: "my-org",
		AppName: "my-app",
		Body: &gen.WorkflowAggregatesBody{
			GroupByStatus: &groupByStatus,
			SelectCount:   &selectCount,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aggs, ok := respObj.(gen.GetWorkflowAggregates200JSONResponse)
	if !ok {
		t.Fatalf("expected gen.GetWorkflowAggregates200JSONResponse, got %T", respObj)
	}
	if len(aggs) != 1 {
		t.Fatalf("expected 1 aggregate, got %d", len(aggs))
	}
	agg := aggs[0]
	if agg.Count == nil || *agg.Count != 42 {
		t.Errorf("expected count 42, got %v", agg.Count)
	}
	if agg.MaxQueueWaitMs == nil || *agg.MaxQueueWaitMs != 150 {
		t.Errorf("expected MaxQueueWaitMs 150, got %v", agg.MaxQueueWaitMs)
	}
	if agg.MaxTotalLatencyMs == nil || *agg.MaxTotalLatencyMs != 500 {
		t.Errorf("expected MaxTotalLatencyMs 500, got %v", agg.MaxTotalLatencyMs)
	}
	if agg.MinCreatedAt == nil || agg.MinCreatedAt.IsZero() {
		t.Error("expected valid MinCreatedAt")
	}
	if agg.Group["status"] == nil || *agg.Group["status"] != "SUCCESS" {
		t.Errorf("expected group status SUCCESS, got %v", agg.Group["status"])
	}
}

func TestGetStepAggregates(t *testing.T) {
	r := &mockWorkflowRouter{
		dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
			req, ok := msg.(*protocol.GetStepAggregatesRequest)
			if !ok {
				t.Fatalf("expected *protocol.GetStepAggregatesRequest, got %T", msg)
			}
			if !req.Body.GroupByFunctionName {
				t.Error("expected GroupByFunctionName true")
			}
			return &protocol.GetStepAggregatesResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeGetStepAggregates,
					RequestID: msg.GetRequestID(),
				},
				Output: []map[string]any{
					{
						"count":           int64(10),
						"function_name":   "processStep",
						"max_duration_ms": int64(120),
					},
				},
			}, nil
		},
	}

	groupByFn := true
	server := api.NewServer(r, nil, nil)
	respObj, err := server.GetStepAggregates(context.Background(), gen.GetStepAggregatesRequestObject{
		OrgName: "my-org",
		AppName: "my-app",
		Body: &gen.StepAggregatesBody{
			GroupByFunctionName: &groupByFn,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aggs, ok := respObj.(gen.GetStepAggregates200JSONResponse)
	if !ok {
		t.Fatalf("expected gen.GetStepAggregates200JSONResponse, got %T", respObj)
	}
	if len(aggs) != 1 {
		t.Fatalf("expected 1 step aggregate, got %d", len(aggs))
	}
	agg := aggs[0]
	if agg.Count == nil || *agg.Count != 10 {
		t.Errorf("expected count 10, got %v", agg.Count)
	}
	if agg.MaxDurationMs == nil || *agg.MaxDurationMs != 120 {
		t.Errorf("expected MaxDurationMs 120, got %v", agg.MaxDurationMs)
	}
	if agg.Group["function_name"] == nil || *agg.Group["function_name"] != "processStep" {
		t.Errorf("expected group function_name processStep, got %v", agg.Group["function_name"])
	}
}
