package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
)

type mockRouter struct {
	dispatchFn func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error)
}

func (m *mockRouter) Dispatch(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
	if m.dispatchFn != nil {
		return m.dispatchFn(ctx, orgName, appName, msg)
	}
	return nil, errors.New("mockRouter: dispatch not implemented")
}

func ptr[T any](v T) *T {
	return &v
}

func TestListQueues(t *testing.T) {
	t.Run("success with queues", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.ListQueuesRequest)
				if !ok {
					t.Fatalf("expected *protocol.ListQueuesRequest, got %T", msg)
				}
				if req.Type != protocol.MessageTypeListQueues {
					t.Errorf("expected MessageTypeListQueues, got %s", req.Type)
				}
				if req.RequestID == "" {
					t.Error("expected non-empty RequestID")
				}

				return &protocol.ListQueuesResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeListQueues,
						RequestID: req.RequestID,
					},
					Output: []protocol.QueueOutput{
						{
							Name:                "queue-1",
							Concurrency:         ptr(10),
							WorkerConcurrency:   ptr(5),
							RateLimitMax:        ptr(100),
							RateLimitPeriodSec:  ptr(60.0),
							PriorityEnabled:     true,
							PartitionQueue:      false,
							PollingIntervalSec: 1.5,
							ApplicationName:     ptr("my-app"),
						},
					},
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.ListQueues(context.Background(), gen.ListQueuesRequestObject{
			OrgName: "my-org",
			AppName: "my-app",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		jsonResp, ok := resp.(gen.ListQueues200JSONResponse)
		if !ok {
			t.Fatalf("expected ListQueues200JSONResponse, got %T", resp)
		}
		if len(jsonResp) != 1 {
			t.Fatalf("expected 1 queue, got %d", len(jsonResp))
		}
		q := jsonResp[0]
		if q.Name != "queue-1" {
			t.Errorf("expected name 'queue-1', got %s", q.Name)
		}
		if q.Concurrency == nil || *q.Concurrency != 10 {
			t.Errorf("expected concurrency 10, got %v", q.Concurrency)
		}
		if q.WorkerConcurrency == nil || *q.WorkerConcurrency != 5 {
			t.Errorf("expected workerConcurrency 5, got %v", q.WorkerConcurrency)
		}
		if q.RateLimitMax == nil || *q.RateLimitMax != 100 {
			t.Errorf("expected rateLimitMax 100, got %v", q.RateLimitMax)
		}
		if q.RateLimitPeriodSecs == nil || *q.RateLimitPeriodSecs != 60.0 {
			t.Errorf("expected rateLimitPeriodSecs 60.0, got %v", q.RateLimitPeriodSecs)
		}
		if !q.PriorityEnabled {
			t.Error("expected priorityEnabled true")
		}
		if q.PartitionQueue {
			t.Error("expected partitionQueue false")
		}
		if q.PollingIntervalSecs != 1.5 {
			t.Errorf("expected pollingIntervalSecs 1.5, got %f", q.PollingIntervalSecs)
		}
		if q.ApplicationName == nil || *q.ApplicationName != "my-app" {
			t.Errorf("expected applicationName 'my-app', got %v", q.ApplicationName)
		}
	})

	t.Run("router error returns problem JSON", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				return nil, router.ErrAppNotFound
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.ListQueues(context.Background(), gen.ListQueuesRequestObject{
			OrgName: "my-org",
			AppName: "nonexistent-app",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.ListQueuesdefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", probResp.StatusCode)
		}
		if probResp.Body.Title == nil || *probResp.Body.Title != "Application not found" {
			t.Errorf("expected title 'Application not found', got %v", probResp.Body.Title)
		}

		rec := httptest.NewRecorder()
		if err := probResp.VisitListQueuesResponse(rec); err != nil {
			t.Fatalf("VisitListQueuesResponse error: %v", err)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("expected recorder status 404, got %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("expected content type application/problem+json, got %s", rec.Header().Get("Content-Type"))
		}
	})

	t.Run("unexpected response type returns 500", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				return &protocol.Envelope{Type: protocol.MessageTypeAlert, RequestID: "req-1"}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.ListQueues(context.Background(), gen.ListQueuesRequestObject{
			OrgName: "my-org",
			AppName: "my-app",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.ListQueuesdefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected status 500, got %d", probResp.StatusCode)
		}
	})
}

func TestGetQueue(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.GetQueueRequest)
				if !ok {
					t.Fatalf("expected *protocol.GetQueueRequest, got %T", msg)
				}
				if req.Name != "order-queue" {
					t.Errorf("expected queue name 'order-queue', got %s", req.Name)
				}

				return &protocol.GetQueueResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeGetQueue,
						RequestID: req.RequestID,
					},
					Output: &protocol.QueueOutput{
						Name:                "order-queue",
						Concurrency:         ptr(20),
						WorkerConcurrency:   ptr(4),
						RateLimitMax:        nil,
						RateLimitPeriodSec:  nil,
						PriorityEnabled:     false,
						PartitionQueue:      true,
						PollingIntervalSec: 2.0,
						ApplicationName:     ptr("my-app"),
					},
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.GetQueue(context.Background(), gen.GetQueueRequestObject{
			OrgName:   "my-org",
			AppName:   "my-app",
			QueueName: "order-queue",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		jsonResp, ok := resp.(gen.GetQueue200JSONResponse)
		if !ok {
			t.Fatalf("expected GetQueue200JSONResponse, got %T", resp)
		}
		if jsonResp.Name != "order-queue" {
			t.Errorf("expected queue name 'order-queue', got %s", jsonResp.Name)
		}
		if !jsonResp.PartitionQueue {
			t.Error("expected partitionQueue true")
		}
	})

	t.Run("queue not found", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.GetQueueRequest)
				return &protocol.GetQueueResponse{
					Envelope: protocol.Envelope{
						Type:         protocol.MessageTypeGetQueue,
						RequestID:    req.RequestID,
						ErrorMessage: ptr("queue 'missing-queue' does not exist"),
					},
					Output: nil,
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.GetQueue(context.Background(), gen.GetQueueRequestObject{
			OrgName:   "my-org",
			AppName:   "my-app",
			QueueName: "missing-queue",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.GetQueuedefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", probResp.StatusCode)
		}
		if probResp.Body.Detail == nil || *probResp.Body.Detail != "queue 'missing-queue' does not exist" {
			t.Errorf("expected detail with error message, got %v", probResp.Body.Detail)
		}
	})

	t.Run("no live executor", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				return nil, router.ErrNoLiveExecutor
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.GetQueue(context.Background(), gen.GetQueueRequestObject{
			OrgName:   "my-org",
			AppName:   "my-app",
			QueueName: "q1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.GetQueuedefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("expected status 503, got %d", probResp.StatusCode)
		}
	})
}

func TestListSchedules(t *testing.T) {
	t.Run("success with filters and various time formats", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		nowStr := now.Format(time.RFC3339)

		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.ListSchedulesRequest)
				if !ok {
					t.Fatalf("expected *protocol.ListSchedulesRequest, got %T", msg)
				}
				if len(req.Body.Status) != 1 || req.Body.Status[0] != "ACTIVE" {
					t.Errorf("expected status ACTIVE, got %v", req.Body.Status)
				}
				if len(req.Body.WorkflowName) != 1 || req.Body.WorkflowName[0] != "myWorkflow" {
					t.Errorf("expected workflowName myWorkflow, got %v", req.Body.WorkflowName)
				}
				if len(req.Body.ScheduleNamePrefix) != 1 || req.Body.ScheduleNamePrefix[0] != "sched-" {
					t.Errorf("expected prefix sched-, got %v", req.Body.ScheduleNamePrefix)
				}
				if req.Body.LoadContext == nil || !*req.Body.LoadContext {
					t.Errorf("expected loadContext true, got %v", req.Body.LoadContext)
				}

				return &protocol.ListSchedulesResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeListSchedules,
						RequestID: req.RequestID,
					},
					Output: []protocol.ScheduleOutput{
						{
							ScheduleID:        "sched-id-1",
							ScheduleName:      "sched-1",
							WorkflowName:      "myWorkflow",
							WorkflowClassName: ptr("MyClass"),
							Schedule:          "0 * * * *",
							Status:            "ACTIVE",
							Context:           ptr(`{"foo":"bar"}`),
							LastFiredAt:       &nowStr,
							AutomaticBackfill: true,
							CronTimezone:      ptr("UTC"),
							QueueName:         ptr("my-queue"),
							ApplicationName:   ptr("my-app"),
						},
						{
							ScheduleID:        "sched-id-2",
							ScheduleName:      "sched-2",
							WorkflowName:      "myWorkflow",
							Schedule:          "*/5 * * * *",
							Status:            "PAUSED",
							LastFiredAt:       ptr("1710000000000"), // epoch millis
							AutomaticBackfill: false,
						},
					},
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.ListSchedules(context.Background(), gen.ListSchedulesRequestObject{
			OrgName: "my-org",
			AppName: "my-app",
			Params: gen.ListSchedulesParams{
				Status:             ptr("ACTIVE"),
				WorkflowName:       ptr("myWorkflow"),
				ScheduleNamePrefix: ptr("sched-"),
				LoadContext:        ptr(true),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		jsonResp, ok := resp.(gen.ListSchedules200JSONResponse)
		if !ok {
			t.Fatalf("expected ListSchedules200JSONResponse, got %T", resp)
		}
		if len(jsonResp) != 2 {
			t.Fatalf("expected 2 schedules, got %d", len(jsonResp))
		}

		s1 := jsonResp[0]
		if s1.ScheduleId != "sched-id-1" {
			t.Errorf("expected scheduleId sched-id-1, got %s", s1.ScheduleId)
		}
		if s1.ScheduleName != "sched-1" {
			t.Errorf("expected scheduleName sched-1, got %s", s1.ScheduleName)
		}
		if s1.CronExpression != "0 * * * *" {
			t.Errorf("expected cronExpression '0 * * * *', got %s", s1.CronExpression)
		}
		if s1.WorkflowClass == nil || *s1.WorkflowClass != "MyClass" {
			t.Errorf("expected workflowClass MyClass, got %v", s1.WorkflowClass)
		}
		if s1.LastFiredAt == nil || !s1.LastFiredAt.Equal(now) {
			t.Errorf("expected lastFiredAt %v, got %v", now, s1.LastFiredAt)
		}
		if !s1.AutomaticBackfill {
			t.Error("expected automaticBackfill true")
		}

		s2 := jsonResp[1]
		if s2.LastFiredAt == nil || s2.LastFiredAt.UnixMilli() != 1710000000000 {
			t.Errorf("expected epoch millis parsing, got %v", s2.LastFiredAt)
		}
	})

	t.Run("router timeout returns 504", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				return nil, router.ErrExecutorTimeout
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.ListSchedules(context.Background(), gen.ListSchedulesRequestObject{
			OrgName: "my-org",
			AppName: "my-app",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.ListSchedulesdefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusGatewayTimeout {
			t.Errorf("expected status 504, got %d", probResp.StatusCode)
		}
	})
}

func TestGetSchedule(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.GetScheduleRequest)
				if !ok {
					t.Fatalf("expected *protocol.GetScheduleRequest, got %T", msg)
				}
				if req.ScheduleName != "daily-report" {
					t.Errorf("expected scheduleName 'daily-report', got %s", req.ScheduleName)
				}

				return &protocol.GetScheduleResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeGetSchedule,
						RequestID: req.RequestID,
					},
					Output: &protocol.ScheduleOutput{
						ScheduleID:   "sched-daily",
						ScheduleName: "daily-report",
						WorkflowName: "generateReport",
						Schedule:     "@daily",
						Status:       "ACTIVE",
					},
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.GetSchedule(context.Background(), gen.GetScheduleRequestObject{
			OrgName:      "my-org",
			AppName:      "my-app",
			ScheduleName: "daily-report",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		jsonResp, ok := resp.(gen.GetSchedule200JSONResponse)
		if !ok {
			t.Fatalf("expected GetSchedule200JSONResponse, got %T", resp)
		}
		if jsonResp.ScheduleName != "daily-report" {
			t.Errorf("expected scheduleName 'daily-report', got %s", jsonResp.ScheduleName)
		}
	})

	t.Run("schedule not found", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.GetScheduleRequest)
				return &protocol.GetScheduleResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeGetSchedule,
						RequestID: req.RequestID,
					},
					Output: nil,
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.GetSchedule(context.Background(), gen.GetScheduleRequestObject{
			OrgName:      "my-org",
			AppName:      "my-app",
			ScheduleName: "unknown",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.GetScheduledefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", probResp.StatusCode)
		}
	})
}

func TestPauseSchedule(t *testing.T) {
	t.Run("success returns 204", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.PauseScheduleRequest)
				if !ok {
					t.Fatalf("expected *protocol.PauseScheduleRequest, got %T", msg)
				}
				if req.ScheduleName != "daily-report" {
					t.Errorf("expected scheduleName 'daily-report', got %s", req.ScheduleName)
				}

				return &protocol.PauseScheduleResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypePauseSchedule,
						RequestID: req.RequestID,
					},
					Success: true,
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.PauseSchedule(context.Background(), gen.PauseScheduleRequestObject{
			OrgName:      "my-org",
			AppName:      "my-app",
			ScheduleName: "daily-report",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, ok := resp.(gen.PauseSchedule204Response)
		if !ok {
			t.Fatalf("expected PauseSchedule204Response, got %T", resp)
		}
	})

	t.Run("failure returns 400", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.PauseScheduleRequest)
				return &protocol.PauseScheduleResponse{
					Envelope: protocol.Envelope{
						Type:         protocol.MessageTypePauseSchedule,
						RequestID:    req.RequestID,
						ErrorMessage: ptr("schedule is already paused"),
					},
					Success: false,
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.PauseSchedule(context.Background(), gen.PauseScheduleRequestObject{
			OrgName:      "my-org",
			AppName:      "my-app",
			ScheduleName: "daily-report",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.PauseScheduledefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", probResp.StatusCode)
		}
		if probResp.Body.Detail == nil || *probResp.Body.Detail != "schedule is already paused" {
			t.Errorf("expected detail 'schedule is already paused', got %v", probResp.Body.Detail)
		}
	})
}

func TestResumeSchedule(t *testing.T) {
	t.Run("success returns 204", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.ResumeScheduleRequest)
				if !ok {
					t.Fatalf("expected *protocol.ResumeScheduleRequest, got %T", msg)
				}
				if req.ScheduleName != "daily-report" {
					t.Errorf("expected scheduleName 'daily-report', got %s", req.ScheduleName)
				}

				return &protocol.ResumeScheduleResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeResumeSchedule,
						RequestID: req.RequestID,
					},
					Success: true,
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.ResumeSchedule(context.Background(), gen.ResumeScheduleRequestObject{
			OrgName:      "my-org",
			AppName:      "my-app",
			ScheduleName: "daily-report",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, ok := resp.(gen.ResumeSchedule204Response)
		if !ok {
			t.Fatalf("expected ResumeSchedule204Response, got %T", resp)
		}
	})

	t.Run("failure returns 400", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.ResumeScheduleRequest)
				return &protocol.ResumeScheduleResponse{
					Envelope: protocol.Envelope{
						Type:         protocol.MessageTypeResumeSchedule,
						RequestID:    req.RequestID,
						ErrorMessage: ptr("schedule is already active"),
					},
					Success: false,
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.ResumeSchedule(context.Background(), gen.ResumeScheduleRequestObject{
			OrgName:      "my-org",
			AppName:      "my-app",
			ScheduleName: "daily-report",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.ResumeScheduledefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", probResp.StatusCode)
		}
	})
}

func TestTriggerSchedule(t *testing.T) {
	t.Run("success returns 201 with workflow ID and Location header", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.TriggerScheduleRequest)
				if !ok {
					t.Fatalf("expected *protocol.TriggerScheduleRequest, got %T", msg)
				}
				if req.ScheduleName != "hourly-cleanup" {
					t.Errorf("expected scheduleName 'hourly-cleanup', got %s", req.ScheduleName)
				}

				return &protocol.TriggerScheduleResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeTriggerSchedule,
						RequestID: req.RequestID,
					},
					WorkflowID: ptr("wf-run-777"),
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.TriggerSchedule(context.Background(), gen.TriggerScheduleRequestObject{
			OrgName:      "test-org",
			AppName:      "test-app",
			ScheduleName: "hourly-cleanup",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		createdResp, ok := resp.(gen.TriggerSchedule201JSONResponse)
		if !ok {
			t.Fatalf("expected TriggerSchedule201JSONResponse, got %T", resp)
		}
		if createdResp.Body.WorkflowId != "wf-run-777" {
			t.Errorf("expected workflowId 'wf-run-777', got %s", createdResp.Body.WorkflowId)
		}
		if createdResp.Headers.Location == nil || *createdResp.Headers.Location != "/v2/orgs/test-org/apps/test-app/workflows/wf-run-777" {
			t.Errorf("expected Location '/v2/orgs/test-org/apps/test-app/workflows/wf-run-777', got %v", createdResp.Headers.Location)
		}

		rec := httptest.NewRecorder()
		if err := createdResp.VisitTriggerScheduleResponse(rec); err != nil {
			t.Fatalf("VisitTriggerScheduleResponse error: %v", err)
		}
		if rec.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d", rec.Code)
		}
		if rec.Header().Get("Location") != "/v2/orgs/test-org/apps/test-app/workflows/wf-run-777" {
			t.Errorf("expected Location header, got %s", rec.Header().Get("Location"))
		}
	})

	t.Run("missing workflow ID returns 400", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.TriggerScheduleRequest)
				return &protocol.TriggerScheduleResponse{
					Envelope: protocol.Envelope{
						Type:         protocol.MessageTypeTriggerSchedule,
						RequestID:    req.RequestID,
						ErrorMessage: ptr("execution failed to spawn workflow"),
					},
					WorkflowID: nil,
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.TriggerSchedule(context.Background(), gen.TriggerScheduleRequestObject{
			OrgName:      "test-org",
			AppName:      "test-app",
			ScheduleName: "hourly-cleanup",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.TriggerScheduledefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", probResp.StatusCode)
		}
	})
}

func TestBackfillSchedule(t *testing.T) {
	t.Run("success returns workflow IDs", func(t *testing.T) {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.BackfillScheduleRequest)
				if !ok {
					t.Fatalf("expected *protocol.BackfillScheduleRequest, got %T", msg)
				}
				if req.ScheduleName != "daily-sync" {
					t.Errorf("expected scheduleName 'daily-sync', got %s", req.ScheduleName)
				}
				if req.Start != start.Format(time.RFC3339) {
					t.Errorf("expected start %s, got %s", start.Format(time.RFC3339), req.Start)
				}
				if req.End != end.Format(time.RFC3339) {
					t.Errorf("expected end %s, got %s", end.Format(time.RFC3339), req.End)
				}

				return &protocol.BackfillScheduleResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeBackfillSchedule,
						RequestID: req.RequestID,
					},
					WorkflowIDs: []string{"wf-bf-1", "wf-bf-2"},
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.BackfillSchedule(context.Background(), gen.BackfillScheduleRequestObject{
			OrgName:      "test-org",
			AppName:      "test-app",
			ScheduleName: "daily-sync",
			Body: &gen.BackfillInputBody{
				StartTime: start,
				EndTime:   end,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		jsonResp, ok := resp.(gen.BackfillSchedule200JSONResponse)
		if !ok {
			t.Fatalf("expected BackfillSchedule200JSONResponse, got %T", resp)
		}
		if len(jsonResp.WorkflowIds) != 2 {
			t.Fatalf("expected 2 workflow IDs, got %d", len(jsonResp.WorkflowIds))
		}
		if jsonResp.WorkflowIds[0] != "wf-bf-1" || jsonResp.WorkflowIds[1] != "wf-bf-2" {
			t.Errorf("unexpected workflow IDs: %v", jsonResp.WorkflowIds)
		}
	})

	t.Run("nil body returns 400", func(t *testing.T) {
		server := NewServer(&mockRouter{}, nil, nil)
		resp, err := server.BackfillSchedule(context.Background(), gen.BackfillScheduleRequestObject{
			OrgName:      "test-org",
			AppName:      "test-app",
			ScheduleName: "daily-sync",
			Body:         nil,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.BackfillScheduledefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", probResp.StatusCode)
		}
	})
}

func TestListMetrics(t *testing.T) {
	t.Run("success returns metrics array", func(t *testing.T) {
		start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
		metricTime := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.GetMetricsRequest)
				if !ok {
					t.Fatalf("expected *protocol.GetMetricsRequest, got %T", msg)
				}
				if req.StartTime != start.Format(time.RFC3339) {
					t.Errorf("expected startTime %s, got %s", start.Format(time.RFC3339), req.StartTime)
				}
				if req.EndTime != end.Format(time.RFC3339) {
					t.Errorf("expected endTime %s, got %s", end.Format(time.RFC3339), req.EndTime)
				}
				if len(req.ApplicationName) != 1 || req.ApplicationName[0] != "analytics-app" {
					t.Errorf("expected applicationName analytics-app, got %v", req.ApplicationName)
				}

				return &protocol.GetMetricsResponse{
					Envelope: protocol.Envelope{
						Type:      protocol.MessageTypeGetMetrics,
						RequestID: req.RequestID,
					},
					Metrics: []protocol.MetricData{
						{
							MetricName:  "dbos_conductor_v1_workflow_started_rate",
							MetricValue: 42.5,
							Timestamp:   metricTime.UnixMilli(),
							Tags: map[string]any{
								"metric_type": "workflow_count",
								"granularity": float64(60),
								"app_id":      "custom-app-id",
							},
						},
						{
							MetricName:  "step_execution_time",
							MetricValue: 120.0,
							Timestamp:   metricTime.Unix(),
							Tags:        nil,
						},
					},
				}, nil
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.ListMetrics(context.Background(), gen.ListMetricsRequestObject{
			OrgName: "analytics-org",
			AppName: "analytics-app",
			Params: gen.ListMetricsParams{
				StartTime: start,
				EndTime:   end,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		jsonResp, ok := resp.(gen.ListMetrics200JSONResponse)
		if !ok {
			t.Fatalf("expected ListMetrics200JSONResponse, got %T", resp)
		}
		if len(jsonResp) != 2 {
			t.Fatalf("expected 2 metrics, got %d", len(jsonResp))
		}

		m1 := jsonResp[0]
		if m1.MetricName != "dbos_conductor_v1_workflow_started_rate" {
			t.Errorf("expected metric name, got %s", m1.MetricName)
		}
		if m1.MetricType != "workflow_count" {
			t.Errorf("expected metricType 'workflow_count', got %s", m1.MetricType)
		}
		if m1.AppId != "custom-app-id" {
			t.Errorf("expected appId 'custom-app-id', got %s", m1.AppId)
		}
		if m1.Granularity != 60 {
			t.Errorf("expected granularity 60, got %d", m1.Granularity)
		}
		if m1.Value != 42 {
			t.Errorf("expected value 42, got %d", m1.Value)
		}
		if !m1.TimeBucket.Equal(metricTime) {
			t.Errorf("expected timeBucket %v, got %v", metricTime, m1.TimeBucket)
		}

		m2 := jsonResp[1]
		if m2.MetricType != "step_count" {
			t.Errorf("expected inferred metricType 'step_count', got %s", m2.MetricType)
		}
		if m2.AppId != "analytics-app" {
			t.Errorf("expected default appId 'analytics-app', got %s", m2.AppId)
		}
		if m2.Value != 120 {
			t.Errorf("expected value 120, got %d", m2.Value)
		}
	})

	t.Run("router error returns problem JSON", func(t *testing.T) {
		r := &mockRouter{
			dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				return nil, router.ErrOrgNotFound
			},
		}

		server := NewServer(r, nil, nil)
		resp, err := server.ListMetrics(context.Background(), gen.ListMetricsRequestObject{
			OrgName: "unknown-org",
			AppName: "analytics-app",
			Params: gen.ListMetricsParams{
				StartTime: time.Now(),
				EndTime:   time.Now(),
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		probResp, ok := resp.(gen.ListMetricsdefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected default problem response, got %T", resp)
		}
		if probResp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", probResp.StatusCode)
		}
	})
}
