package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool {
	return &b
}

func int64Ptr(i int64) *int64 {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}

func getAllGoldenFixtures() map[string]struct {
	isResponse bool
	expected   Message
} {
	return map[string]struct {
		isResponse bool
		expected   Message
	}{
		"alert_request.json": {
			isResponse: false,
			expected: &AlertRequest{
				Envelope: Envelope{Type: MessageTypeAlert, RequestID: "req-uuid"},
				Name:     "system_failure",
				Message:  "High memory usage",
				Metadata: map[string]string{"cpu": "90%"},
			},
		},
		"alert_response.json": {
			isResponse: true,
			expected: &AlertResponse{
				Envelope: Envelope{Type: MessageTypeAlert, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"backfill_schedule_request.json": {
			isResponse: false,
			expected: &BackfillScheduleRequest{
				Envelope:     Envelope{Type: MessageTypeBackfillSchedule, RequestID: "req-uuid"},
				ScheduleName: "daily-report",
				Start:        "2026-09-01T00:00:00Z",
				End:          "2026-09-02T00:00:00Z",
			},
		},
		"backfill_schedule_response.json": {
			isResponse: true,
			expected: &BackfillScheduleResponse{
				Envelope:    Envelope{Type: MessageTypeBackfillSchedule, RequestID: "req-uuid"},
				WorkflowIDs: []string{"wf-bf-1", "wf-bf-2"},
			},
		},
		"cancel_workflow_request.json": {
			isResponse: false,
			expected: &CancelWorkflowRequest{
				Envelope:       Envelope{Type: MessageTypeCancel, RequestID: "req-uuid"},
				WorkflowID:     "wf-123",
				WorkflowIDs:    []string{"wf-123"},
				CancelChildren: true,
			},
		},
		"cancel_workflow_response.json": {
			isResponse: true,
			expected: &CancelWorkflowResponse{
				Envelope: Envelope{Type: MessageTypeCancel, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"delete_workflow_request.json": {
			isResponse: false,
			expected: &DeleteWorkflowRequest{
				Envelope:       Envelope{Type: MessageTypeDelete, RequestID: "req-uuid"},
				WorkflowID:     "wf-123",
				WorkflowIDs:    []string{"wf-123"},
				DeleteChildren: true,
			},
		},
		"delete_workflow_response.json": {
			isResponse: true,
			expected: &DeleteWorkflowResponse{
				Envelope: Envelope{Type: MessageTypeDelete, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"executor_info_request.json": {
			isResponse: false,
			expected: &ExecutorInfoRequest{
				Envelope: Envelope{Type: MessageTypeExecutorInfo, RequestID: "req-uuid"},
			},
		},
		"executor_info_response.json": {
			isResponse: true,
			expected: &ExecutorInfoResponse{
				Envelope:           Envelope{Type: MessageTypeExecutorInfo, RequestID: "req-uuid"},
				ExecutorID:         "exec-123",
				ApplicationVersion: "v1.0.0",
				Hostname:           stringPtr("localhost"),
				Language:           "go",
				DBOSVersion:        "0.1.0",
			},
		},
		"exist_pending_workflows_request.json": {
			isResponse: false,
			expected: &ExistPendingWorkflowsRequest{
				Envelope:           Envelope{Type: MessageTypeExistPendingWorkflows, RequestID: "req-uuid"},
				ExecutorID:         "exec-123",
				ApplicationVersion: "v1.0.0",
			},
		},
		"exist_pending_workflows_response.json": {
			isResponse: true,
			expected: &ExistPendingWorkflowsResponse{
				Envelope: Envelope{Type: MessageTypeExistPendingWorkflows, RequestID: "req-uuid"},
				Exist:    true,
			},
		},
		"export_workflow_request.json": {
			isResponse: false,
			expected: &ExportWorkflowRequest{
				Envelope:       Envelope{Type: MessageTypeExportWorkflow, RequestID: "req-uuid"},
				WorkflowID:     "wf-123",
				ExportChildren: true,
			},
		},
		"export_workflow_response.json": {
			isResponse: true,
			expected: &ExportWorkflowResponse{
				Envelope:           Envelope{Type: MessageTypeExportWorkflow, RequestID: "req-uuid"},
				SerializedWorkflow: stringPtr("{\"version\":1}"),
			},
		},
		"fork_from_failure_request.json": {
			isResponse: false,
			expected: &ForkFromFailureRequest{
				Envelope: Envelope{Type: MessageTypeForkFromFailure, RequestID: "req-uuid"},
				Body: ForkFromFailureRequestBody{
					WorkflowIDs:        []string{"wf-123"},
					ApplicationVersion: stringPtr("v1.0.0"),
					FromLastFailure:    true,
				},
			},
		},
		"fork_from_failure_response.json": {
			isResponse: true,
			expected: &ForkFromFailureResponse{
				Envelope:          Envelope{Type: MessageTypeForkFromFailure, RequestID: "req-uuid"},
				ForkedWorkflowIDs: []string{"wf-forked-1"},
			},
		},
		"fork_workflow_request.json": {
			isResponse: false,
			expected: &ForkWorkflowRequest{
				Envelope: Envelope{Type: MessageTypeForkWorkflow, RequestID: "req-uuid"},
				Body: ForkWorkflowRequestBody{
					WorkflowID:         "wf-123",
					StartStep:          2,
					ApplicationVersion: stringPtr("v1.0.0"),
					NewWorkflowID:      stringPtr("wf-forked-2"),
				},
			},
		},
		"fork_workflow_response.json": {
			isResponse: true,
			expected: &ForkWorkflowResponse{
				Envelope:      Envelope{Type: MessageTypeForkWorkflow, RequestID: "req-uuid"},
				NewWorkflowID: stringPtr("wf-forked-2"),
			},
		},
		"get_metrics_request.json": {
			isResponse: false,
			expected: &GetMetricsRequest{
				Envelope:        Envelope{Type: MessageTypeGetMetrics, RequestID: "req-uuid"},
				StartTime:       "2026-09-01T00:00:00Z",
				EndTime:         "2026-09-02T00:00:00Z",
				MetricClass:     "workflow_count",
				ApplicationName: []string{"sample-app"},
			},
		},
		"get_metrics_response.json": {
			isResponse: true,
			expected: &GetMetricsResponse{
				Envelope: Envelope{Type: MessageTypeGetMetrics, RequestID: "r1"},
				Metrics: []MetricData{
					{
						MetricName: "dbos_workflows",
						MetricType: "counter",
						Value:      1,
					},
				},
			},
		},
		"get_queue_request.json": {
			isResponse: false,
			expected: &GetQueueRequest{
				Envelope: Envelope{Type: MessageTypeGetQueue, RequestID: "req-uuid"},
				Name:     "default-queue",
			},
		},
		"get_queue_response.json": {
			isResponse: true,
			expected: &GetQueueResponse{
				Envelope: Envelope{Type: MessageTypeGetQueue, RequestID: "req-uuid"},
				Output: &QueueOutput{
					Name:               "default-queue",
					Concurrency:        intPtr(10),
					WorkerConcurrency:  intPtr(2),
					RateLimitMax:       intPtr(100),
					RateLimitPeriodSec: float64Ptr(60),
					PriorityEnabled:    true,
					PartitionQueue:     false,
					PollingIntervalSec: 1.5,
					ApplicationName:    stringPtr("sample-app"),
				},
			},
		},
		"get_schedule_request.json": {
			isResponse: false,
			expected: &GetScheduleRequest{
				Envelope:     Envelope{Type: MessageTypeGetSchedule, RequestID: "req-uuid"},
				ScheduleName: "daily-report",
				LoadContext:  boolPtr(true),
			},
		},
		"get_schedule_response.json": {
			isResponse: true,
			expected: &GetScheduleResponse{
				Envelope: Envelope{Type: MessageTypeGetSchedule, RequestID: "req-uuid"},
				Output: &ScheduleOutput{
					ScheduleID:        "sched-123",
					ScheduleName:      "daily-report",
					WorkflowName:      "report_wf",
					WorkflowClassName: stringPtr("Reports"),
					Schedule:          "0 0 * * *",
					Status:            "ACTIVE",
					Context:           stringPtr("{}"),
					LastFiredAt:       stringPtr("2026-09-01T00:00:00Z"),
					AutomaticBackfill: true,
					CronTimezone:      stringPtr("UTC"),
					QueueName:         stringPtr("sched-queue"),
					ApplicationName:   stringPtr("sample-app"),
				},
			},
		},
		"get_step_aggregates_request.json": {
			isResponse: false,
			expected: &GetStepAggregatesRequest{
				Envelope: Envelope{Type: MessageTypeGetStepAggregates, RequestID: "req-uuid"},
				Body: GetStepAggregatesRequestBody{
					GroupByFunctionName: true,
					GroupByStatus:       false,
					SelectCount:         true,
					SelectMaxDurationMs: true,
					FunctionName:        StringOrList{"step-1"},
				},
			},
		},
		"get_step_aggregates_response.json": {
			isResponse: true,
			expected: &GetStepAggregatesResponse{
				Envelope: Envelope{Type: MessageTypeGetStepAggregates, RequestID: "req-uuid"},
				Output: []StepAggregateRow{
					{
						Group:         map[string]*string{"function_name": stringPtr("step-1")},
						Count:         int64Ptr(5),
						MaxDurationMs: int64Ptr(120),
					},
				},
			},
		},
		"get_workflow_aggregates_request.json": {
			isResponse: false,
			expected: &GetWorkflowAggregatesRequest{
				Envelope: Envelope{Type: MessageTypeGetWorkflowAggregates, RequestID: "req-uuid"},
				Body: GetWorkflowAggregatesRequestBody{
					GroupByStatus:           true,
					SelectCount:             true,
					SelectMinCreatedAt:      true,
					SelectMaxQueueWaitMs:    true,
					SelectMaxTotalLatencyMs: true,
					Status:                  StringOrList{"SUCCESS"},
				},
			},
		},
		"get_workflow_aggregates_response.json": {
			isResponse: true,
			expected: &GetWorkflowAggregatesResponse{
				Envelope: Envelope{Type: MessageTypeGetWorkflowAggregates, RequestID: "req-uuid"},
				Output: []WorkflowAggregateRow{
					{
						Group:             map[string]*string{"status": stringPtr("SUCCESS")},
						Count:             int64Ptr(42),
						MinCreatedAt:      int64Ptr(1756713600000),
						MaxQueueWaitMs:    int64Ptr(150),
						MaxTotalLatencyMs: int64Ptr(500),
					},
				},
			},
		},
		"get_workflow_events_request.json": {
			isResponse: false,
			expected: &GetWorkflowEventsRequest{
				Envelope:   Envelope{Type: MessageTypeGetWorkflowEvents, RequestID: "req-uuid"},
				WorkflowID: "wf-123",
			},
		},
		"get_workflow_events_response.json": {
			isResponse: true,
			expected: &GetWorkflowEventsResponse{
				Envelope: Envelope{Type: MessageTypeGetWorkflowEvents, RequestID: "req-uuid"},
				Events: []EventOutput{
					{Key: "order_confirmed", Value: "{\"order_id\":\"ord-1\"}"},
				},
			},
		},
		"get_workflow_notifications_request.json": {
			isResponse: false,
			expected: &GetWorkflowNotificationsRequest{
				Envelope:   Envelope{Type: MessageTypeGetWorkflowNotifications, RequestID: "req-uuid"},
				WorkflowID: "wf-123",
			},
		},
		"get_workflow_notifications_response.json": {
			isResponse: true,
			expected: &GetWorkflowNotificationsResponse{
				Envelope: Envelope{Type: MessageTypeGetWorkflowNotifications, RequestID: "req-uuid"},
				Notifications: []NotificationOutput{
					{
						Topic:            stringPtr("billing"),
						Message:          "invoice-generated",
						CreatedAtEpochMs: 1756713600000,
						Consumed:         false,
					},
				},
			},
		},
		"get_workflow_request.json": {
			isResponse: false,
			expected: &GetWorkflowRequest{
				Envelope:   Envelope{Type: MessageTypeGetWorkflow, RequestID: "req-uuid"},
				WorkflowID: "wf-123",
				LoadInput:  true,
				LoadOutput: true,
			},
		},
		"get_workflow_response.json": {
			isResponse: true,
			expected: &GetWorkflowResponse{
				Envelope: Envelope{Type: MessageTypeGetWorkflow, RequestID: "req-uuid"},
				Output: &ListWorkflowsResponseBody{
					WorkflowUUID: "wf-123",
					Status:       stringPtr("PENDING"),
				},
			},
		},
		"get_workflow_streams_request.json": {
			isResponse: false,
			expected: &GetWorkflowStreamsRequest{
				Envelope:   Envelope{Type: MessageTypeGetWorkflowStreams, RequestID: "req-uuid"},
				WorkflowID: "wf-123",
			},
		},
		"get_workflow_streams_response.json": {
			isResponse: true,
			expected: &GetWorkflowStreamsResponse{
				Envelope: Envelope{Type: MessageTypeGetWorkflowStreams, RequestID: "req-uuid"},
				Streams: []StreamEntryOutput{
					{Key: "sensor-stream", Values: []string{"v1", "v2"}},
				},
			},
		},
		"import_workflow_request.json": {
			isResponse: false,
			expected: &ImportWorkflowRequest{
				Envelope:           Envelope{Type: MessageTypeImportWorkflow, RequestID: "req-uuid"},
				SerializedWorkflow: "{\"version\":1}",
			},
		},
		"import_workflow_response.json": {
			isResponse: true,
			expected: &ImportWorkflowResponse{
				Envelope: Envelope{Type: MessageTypeImportWorkflow, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"list_application_versions_request.json": {
			isResponse: false,
			expected: &ListApplicationVersionsRequest{
				Envelope: Envelope{Type: MessageTypeListApplicationVersions, RequestID: "req-uuid"},
			},
		},
		"list_application_versions_response.json": {
			isResponse: true,
			expected: &ListApplicationVersionsResponse{
				Envelope: Envelope{Type: MessageTypeListApplicationVersions, RequestID: "req-uuid"},
				Output: []ApplicationVersionOutput{
					{
						ID:        "ver-1",
						Name:      "v1.0.0",
						Timestamp: 1756713600000,
						CreatedAt: 1756713600000,
					},
				},
			},
		},
		"list_queues_request.json": {
			isResponse: false,
			expected: &ListQueuesRequest{
				Envelope: Envelope{Type: MessageTypeListQueues, RequestID: "req-uuid"},
				Body:     ListQueuesRequestBody{ApplicationName: StringOrList{"sample-app"}},
			},
		},
		"list_queues_response.json": {
			isResponse: true,
			expected: &ListQueuesResponse{
				Envelope: Envelope{Type: MessageTypeListQueues, RequestID: "req-uuid"},
				Output: []QueueOutput{
					{
						Name:               "default-queue",
						Concurrency:        intPtr(5),
						WorkerConcurrency:  intPtr(1),
						PriorityEnabled:    false,
						PartitionQueue:     false,
						PollingIntervalSec: 1,
						ApplicationName:    stringPtr("sample-app"),
					},
				},
			},
		},
		"list_queued_workflows_request.json": {
			isResponse: false,
			expected: &ListWorkflowsRequest{
				Envelope: Envelope{Type: MessageTypeListQueuedWorkflows, RequestID: "req-uuid"},
				Body: ListWorkflowsRequestBody{
					QueueName:  StringOrList{"order-queue"},
					QueuesOnly: true,
				},
			},
		},
		"list_queued_workflows_response.json": {
			isResponse: true,
			expected: &ListWorkflowsResponse{
				Envelope: Envelope{Type: MessageTypeListQueuedWorkflows, RequestID: "req-uuid"},
				Output: []ListWorkflowsResponseBody{
					{
						WorkflowUUID: "wf-q-1",
						Status:       stringPtr("ENQUEUED"),
						QueueName:    stringPtr("order-queue"),
					},
				},
			},
		},
		"list_schedules_request.json": {
			isResponse: false,
			expected: &ListSchedulesRequest{
				Envelope: Envelope{Type: MessageTypeListSchedules, RequestID: "req-uuid"},
				Body: ListSchedulesRequestBody{
					Status:          StringOrList{"ACTIVE"},
					ApplicationName: StringOrList{"sample-app"},
				},
			},
		},
		"list_schedules_response.json": {
			isResponse: true,
			expected: &ListSchedulesResponse{
				Envelope: Envelope{Type: MessageTypeListSchedules, RequestID: "req-uuid"},
				Output: []ScheduleOutput{
					{
						ScheduleID:        "sched-1",
						ScheduleName:      "daily-report",
						WorkflowName:      "report_wf",
						Schedule:          "0 0 * * *",
						Status:            "ACTIVE",
						AutomaticBackfill: false,
						ApplicationName:   stringPtr("sample-app"),
					},
				},
			},
		},
		"list_steps_request.json": {
			isResponse: false,
			expected: &ListStepsRequest{
				Envelope:   Envelope{Type: MessageTypeListSteps, RequestID: "req-uuid"},
				WorkflowID: "wf-123",
				LoadOutput: true,
			},
		},
		"list_steps_response.json": {
			isResponse: true,
			expected: &ListStepsResponse{
				Envelope: Envelope{Type: MessageTypeListSteps, RequestID: "req-uuid"},
				Output: &[]WorkflowStepsResponseBody{
					{
						FunctionID:   1,
						FunctionName: "processPayment",
						Output:       stringPtr("{\"status\":\"ok\"}"),
					},
				},
			},
		},
		"list_workflows_request.json": {
			isResponse: false,
			expected: &ListWorkflowsRequest{
				Envelope: Envelope{Type: MessageTypeListWorkflows, RequestID: "req-uuid"},
				Body: ListWorkflowsRequestBody{
					WorkflowName: StringOrList{"my_workflow"},
					Limit:        intPtr(10),
				},
			},
		},
		"list_workflows_response.json": {
			isResponse: true,
			expected: &ListWorkflowsResponse{
				Envelope: Envelope{Type: MessageTypeListWorkflows, RequestID: "req-uuid"},
				Output: []ListWorkflowsResponseBody{
					{
						WorkflowUUID: "wf-123",
						Status:       stringPtr("SUCCESS"),
					},
				},
			},
		},
		"pause_schedule_request.json": {
			isResponse: false,
			expected: &PauseScheduleRequest{
				Envelope:     Envelope{Type: MessageTypePauseSchedule, RequestID: "req-uuid"},
				ScheduleName: "daily-report",
			},
		},
		"pause_schedule_response.json": {
			isResponse: true,
			expected: &PauseScheduleResponse{
				Envelope: Envelope{Type: MessageTypePauseSchedule, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"recovery_request.json": {
			isResponse: false,
			expected: &RecoveryRequest{
				Envelope:    Envelope{Type: MessageTypeRecovery, RequestID: "req-uuid"},
				ExecutorIDs: []string{"exec-123"},
			},
		},
		"recovery_response.json": {
			isResponse: true,
			expected: &RecoveryResponse{
				Envelope: Envelope{Type: MessageTypeRecovery, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"resume_schedule_request.json": {
			isResponse: false,
			expected: &ResumeScheduleRequest{
				Envelope:     Envelope{Type: MessageTypeResumeSchedule, RequestID: "req-uuid"},
				ScheduleName: "daily-report",
			},
		},
		"resume_schedule_response.json": {
			isResponse: true,
			expected: &ResumeScheduleResponse{
				Envelope: Envelope{Type: MessageTypeResumeSchedule, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"resume_workflow_request.json": {
			isResponse: false,
			expected: &ResumeWorkflowRequest{
				Envelope:    Envelope{Type: MessageTypeResume, RequestID: "req-uuid"},
				WorkflowID:  "wf-123",
				WorkflowIDs: []string{"wf-123"},
				QueueName:   stringPtr("recovery-queue"),
			},
		},
		"resume_workflow_response.json": {
			isResponse: true,
			expected: &ResumeWorkflowResponse{
				Envelope: Envelope{Type: MessageTypeResume, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"retention_request.json": {
			isResponse: false,
			expected: &RetentionRequest{
				Envelope: Envelope{Type: MessageTypeRetention, RequestID: "req-uuid"},
				Body: RetentionRequestBody{
					GCCutoffEpochMs:      intPtr(1725753600000),
					GCRowsThreshold:      intPtr(50000),
					GCBatchSize:          intPtr(10000),
					TimeoutCutoffEpochMs: intPtr(1725750000000),
				},
			},
		},
		"retention_response.json": {
			isResponse: true,
			expected: &RetentionResponse{
				Envelope: Envelope{Type: MessageTypeRetention, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"set_latest_application_version_request.json": {
			isResponse: false,
			expected: &SetLatestApplicationVersionRequest{
				Envelope:    Envelope{Type: MessageTypeSetLatestApplicationVersion, RequestID: "req-uuid"},
				VersionName: "v2.0.0",
			},
		},
		"set_latest_application_version_response.json": {
			isResponse: true,
			expected: &SetLatestApplicationVersionResponse{
				Envelope: Envelope{Type: MessageTypeSetLatestApplicationVersion, RequestID: "req-uuid"},
				Success:  true,
			},
		},
		"trigger_schedule_request.json": {
			isResponse: false,
			expected: &TriggerScheduleRequest{
				Envelope:     Envelope{Type: MessageTypeTriggerSchedule, RequestID: "req-uuid"},
				ScheduleName: "daily-report",
			},
		},
		"trigger_schedule_response.json": {
			isResponse: true,
			expected: &TriggerScheduleResponse{
				Envelope:   Envelope{Type: MessageTypeTriggerSchedule, RequestID: "req-uuid"},
				WorkflowID: stringPtr("wf-trig-1"),
			},
		},
	}
}

func TestDecodeEncodeRoundTrip(t *testing.T) {
	fixtures := getAllGoldenFixtures()
	for name, tt := range fixtures {
		t.Run(name, func(t *testing.T) {
			encoded, err := Encode(tt.expected)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}
			var decoded Message
			if tt.isResponse {
				decoded, err = DecodeResponse(encoded)
			} else {
				decoded, err = DecodeRequest(encoded)
			}
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}
			reEncoded, err := Encode(decoded)
			if err != nil {
				t.Fatalf("Re-encode failed: %v", err)
			}
			if string(encoded) != string(reEncoded) {
				t.Errorf("Roundtrip mismatch: got %s, want %s", string(reEncoded), string(encoded))
			}
		})
	}
}

func TestGoldenFiles(t *testing.T) {
	files, err := filepath.Glob("testdata/golden/*.json")
	if err != nil {
		t.Fatalf("Failed to glob golden files: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("No golden files found")
	}

	expectedMap := getAllGoldenFixtures()
	if len(files) != len(expectedMap) {
		t.Errorf("Expected %d golden files, found %d", len(expectedMap), len(files))
	}

	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("Failed to read golden file: %v", err)
			}

			exp, ok := expectedMap[name]
			if !ok {
				t.Fatalf("No expected definition for golden fixture %s", name)
			}

			// 1. Strict decode with DisallowUnknownFields into a fresh target of the expected type
			targetVal := reflect.New(reflect.TypeOf(exp.expected).Elem()).Interface()
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			if err := dec.Decode(targetVal); err != nil {
				t.Fatalf("Strict decode (DisallowUnknownFields) failed: %v", err)
			}

			// 2. Decode using DecodeRequest / DecodeResponse according to direction
			var msg Message
			if exp.isResponse {
				msg, err = DecodeResponse(data)
			} else {
				msg, err = DecodeRequest(data)
			}
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			// 3. Assert concrete type matches expected
			if reflect.TypeOf(msg) != reflect.TypeOf(exp.expected) {
				t.Fatalf("Type mismatch: got %T, want %T", msg, exp.expected)
			}

			// 4. Assert field values match expected
			if !reflect.DeepEqual(msg, exp.expected) {
				t.Errorf("Field value mismatch: got %+v, want %+v", msg, exp.expected)
			}

			// 5. Normalized JSON comparison
			encoded, err := Encode(msg)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}
			var origJSON, reencJSON any
			if err := json.Unmarshal(data, &origJSON); err != nil {
				t.Fatalf("Unmarshal orig JSON failed: %v", err)
			}
			if err := json.Unmarshal(encoded, &reencJSON); err != nil {
				t.Fatalf("Unmarshal reencoded JSON failed: %v", err)
			}
			if !reflect.DeepEqual(origJSON, reencJSON) {
				t.Errorf("Normalized JSON mismatch: got %+v, want %+v", reencJSON, origJSON)
			}
		})
	}
}

func TestStringOrList(t *testing.T) {
	var single StringOrList
	err := json.Unmarshal([]byte(`"val1"`), &single)
	if err != nil {
		t.Fatalf("Failed to parse single string: %v", err)
	}
	if len(single) != 1 || single[0] != "val1" {
		t.Errorf("Unexpected single parsing: %v", single)
	}

	var list StringOrList
	err = json.Unmarshal([]byte(`["val1", "val2"]`), &list)
	if err != nil {
		t.Fatalf("Failed to parse list of strings: %v", err)
	}
	if len(list) != 2 || list[1] != "val2" {
		t.Errorf("Unexpected list parsing: %v", list)
	}

	var empty StringOrList
	err = json.Unmarshal([]byte(`null`), &empty)
	if err != nil {
		t.Fatalf("Failed to parse null: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("Unexpected null parsing: %v", empty)
	}
}

func TestErrorResponses(t *testing.T) {
	errMsg := "some error"
	data := []byte(`{"type":"executor_info", "request_id":"123", "error_message":"some error"}`)
	msg, err := DecodeResponse(data)
	if err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}
	resp, ok := msg.(*ExecutorInfoResponse)
	if !ok {
		t.Fatalf("Expected ExecutorInfoResponse, got %T", msg)
	}
	if resp.ErrorMessage == nil || *resp.ErrorMessage != errMsg {
		t.Errorf("Expected ErrorMessage 'some error', got %v", resp.ErrorMessage)
	}
}

func TestMalformedPayload(t *testing.T) {
	// Missing type
	_, err := DecodeRequest([]byte(`{"request_id":"1"}`))
	if err == nil {
		t.Errorf("Expected error for missing type")
	}

	// Invalid JSON
	_, err = DecodeRequest([]byte(`{foo}`))
	if err == nil {
		t.Errorf("Expected error for invalid JSON")
	}

	// Unknown type
	_, err = DecodeRequest([]byte(`{"type":"unknown_type", "request_id":"1"}`))
	if err == nil {
		t.Errorf("Expected error for unknown type")
	}
}

func TestZeroValueResponses(t *testing.T) {
	responses := []Message{
		&ExecutorInfoResponse{Envelope: Envelope{Type: MessageTypeExecutorInfo}},
		&RecoveryResponse{Envelope: Envelope{Type: MessageTypeRecovery}},
		&CancelWorkflowResponse{Envelope: Envelope{Type: MessageTypeCancel}},
		&ResumeWorkflowResponse{Envelope: Envelope{Type: MessageTypeResume}},
		&ListWorkflowsResponse{Envelope: Envelope{Type: MessageTypeListWorkflows}},
		&ListStepsResponse{Envelope: Envelope{Type: MessageTypeListSteps}},
		&GetWorkflowResponse{Envelope: Envelope{Type: MessageTypeGetWorkflow}},
		&ForkWorkflowResponse{Envelope: Envelope{Type: MessageTypeForkWorkflow}},
		&ForkFromFailureResponse{Envelope: Envelope{Type: MessageTypeForkFromFailure}},
		&ExistPendingWorkflowsResponse{Envelope: Envelope{Type: MessageTypeExistPendingWorkflows}},
		&RetentionResponse{Envelope: Envelope{Type: MessageTypeRetention}},
		&GetMetricsResponse{Envelope: Envelope{Type: MessageTypeGetMetrics}},
		&ExportWorkflowResponse{Envelope: Envelope{Type: MessageTypeExportWorkflow}},
		&ImportWorkflowResponse{Envelope: Envelope{Type: MessageTypeImportWorkflow}},
		&DeleteWorkflowResponse{Envelope: Envelope{Type: MessageTypeDelete}},
		&AlertResponse{Envelope: Envelope{Type: MessageTypeAlert}},
		&ListSchedulesResponse{Envelope: Envelope{Type: MessageTypeListSchedules}},
		&GetScheduleResponse{Envelope: Envelope{Type: MessageTypeGetSchedule}},
		&PauseScheduleResponse{Envelope: Envelope{Type: MessageTypePauseSchedule}},
		&ResumeScheduleResponse{Envelope: Envelope{Type: MessageTypeResumeSchedule}},
		&BackfillScheduleResponse{Envelope: Envelope{Type: MessageTypeBackfillSchedule}},
		&TriggerScheduleResponse{Envelope: Envelope{Type: MessageTypeTriggerSchedule}},
		&GetWorkflowEventsResponse{Envelope: Envelope{Type: MessageTypeGetWorkflowEvents}},
		&GetWorkflowNotificationsResponse{Envelope: Envelope{Type: MessageTypeGetWorkflowNotifications}},
		&GetWorkflowStreamsResponse{Envelope: Envelope{Type: MessageTypeGetWorkflowStreams}},
		&GetWorkflowAggregatesResponse{Envelope: Envelope{Type: MessageTypeGetWorkflowAggregates}},
		&GetStepAggregatesResponse{Envelope: Envelope{Type: MessageTypeGetStepAggregates}},
		&ListApplicationVersionsResponse{Envelope: Envelope{Type: MessageTypeListApplicationVersions}},
		&SetLatestApplicationVersionResponse{Envelope: Envelope{Type: MessageTypeSetLatestApplicationVersion}},
		&ListQueuesResponse{Envelope: Envelope{Type: MessageTypeListQueues}},
		&GetQueueResponse{Envelope: Envelope{Type: MessageTypeGetQueue}},
		&ListWorkflowsResponse{Envelope: Envelope{Type: MessageTypeListQueuedWorkflows}}, // Map queued workflows too
	}

	for _, msg := range responses {
		t.Run(string(msg.GetMessageType()), func(t *testing.T) {
			encoded, err := Encode(msg)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			decoded, err := DecodeResponse(encoded)
			if err != nil {
				t.Fatalf("DecodeResponse failed for zero-value %T: %v", msg, err)
			}
			if reflect.TypeOf(decoded) != reflect.TypeOf(msg) {
				t.Fatalf("DecodeResponse misclassified: got %T, want %T", decoded, msg)
			}
		})
	}
}

func TestDecodeHeuristicFallback(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantType reflect.Type
	}{
		{
			name:     "executor_info with executor_id",
			payload:  `{"type":"executor_info","request_id":"r1","executor_id":"e1"}`,
			wantType: reflect.TypeOf(&ExecutorInfoResponse{}),
		},
		{
			name:     "fork_workflow with new_workflow_id",
			payload:  `{"type":"fork_workflow","request_id":"r2","new_workflow_id":"wf-fork"}`,
			wantType: reflect.TypeOf(&ForkWorkflowResponse{}),
		},
		{
			name:     "fork_from_failure with forked_workflow_ids",
			payload:  `{"type":"fork_from_failure","request_id":"r3","forked_workflow_ids":["wf-1"]}`,
			wantType: reflect.TypeOf(&ForkFromFailureResponse{}),
		},
		{
			name:     "exist_pending_workflows with exist",
			payload:  `{"type":"exist_pending_workflows","request_id":"r4","exist":true}`,
			wantType: reflect.TypeOf(&ExistPendingWorkflowsResponse{}),
		},
		{
			name:     "get_metrics with metrics",
			payload:  `{"type":"get_metrics","request_id":"r5","metrics":[]}`,
			wantType: reflect.TypeOf(&GetMetricsResponse{}),
		},
		{
			name:     "export_workflow with serialized_workflow",
			payload:  `{"type":"export_workflow","request_id":"r6","serialized_workflow":"data"}`,
			wantType: reflect.TypeOf(&ExportWorkflowResponse{}),
		},
		{
			name:     "backfill_schedule with workflow_ids",
			payload:  `{"type":"backfill_schedule","request_id":"r7","workflow_ids":["wf-1"]}`,
			wantType: reflect.TypeOf(&BackfillScheduleResponse{}),
		},
		{
			name:     "trigger_schedule with workflow_id",
			payload:  `{"type":"trigger_schedule","request_id":"r8","workflow_id":"wf-1"}`,
			wantType: reflect.TypeOf(&TriggerScheduleResponse{}),
		},
		{
			name:     "get_workflow_events with events",
			payload:  `{"type":"get_workflow_events","request_id":"r9","events":[]}`,
			wantType: reflect.TypeOf(&GetWorkflowEventsResponse{}),
		},
		{
			name:     "get_workflow_notifications with notifications",
			payload:  `{"type":"get_workflow_notifications","request_id":"r10","notifications":[]}`,
			wantType: reflect.TypeOf(&GetWorkflowNotificationsResponse{}),
		},
		{
			name:     "get_workflow_streams with streams",
			payload:  `{"type":"get_workflow_streams","request_id":"r11","streams":[]}`,
			wantType: reflect.TypeOf(&GetWorkflowStreamsResponse{}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := Decode([]byte(tt.payload))
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}
			if reflect.TypeOf(msg) != tt.wantType {
				t.Fatalf("Decode returned %T, want %v", msg, tt.wantType)
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
}

func stringPtr(s string) *string {
	return &s
}

func TestSpecCoversAllMessageTypes(t *testing.T) {
	docBytes, err := os.ReadFile("../../docs/protocol/executor-ws.md")
	if err != nil {
		t.Fatalf("failed to read executor-ws.md: %v", err)
	}
	doc := string(docBytes)

	messageTypes := []MessageType{
		MessageTypeExecutorInfo,
		MessageTypeRecovery,
		MessageTypeCancel,
		MessageTypeResume,
		MessageTypeListWorkflows,
		MessageTypeListQueuedWorkflows,
		MessageTypeListSteps,
		MessageTypeGetWorkflow,
		MessageTypeForkWorkflow,
		MessageTypeForkFromFailure,
		MessageTypeExistPendingWorkflows,
		MessageTypeRetention,
		MessageTypeGetMetrics,
		MessageTypeExportWorkflow,
		MessageTypeImportWorkflow,
		MessageTypeDelete,
		MessageTypeAlert,
		MessageTypeListSchedules,
		MessageTypeGetSchedule,
		MessageTypePauseSchedule,
		MessageTypeResumeSchedule,
		MessageTypeBackfillSchedule,
		MessageTypeTriggerSchedule,
		MessageTypeGetWorkflowEvents,
		MessageTypeGetWorkflowNotifications,
		MessageTypeGetWorkflowStreams,
		MessageTypeGetWorkflowAggregates,
		MessageTypeGetStepAggregates,
		MessageTypeListApplicationVersions,
		MessageTypeSetLatestApplicationVersion,
		MessageTypeListQueues,
		MessageTypeGetQueue,
	}

	for _, mt := range messageTypes {
		if !strings.Contains(doc, string(mt)) {
			t.Errorf("expected doc to mention message type %q", mt)
		}
	}
}

func TestAggregateFrameDecoding(t *testing.T) {
	wfFrame := []byte(`{
		"type": "get_workflow_aggregates",
		"request_id": "req-agg-1",
		"output": [
			{
				"group": {"status": "SUCCESS"},
				"count": 42,
				"min_created_at": 1756713600000,
				"max_queue_wait_ms": 150,
				"max_total_latency_ms": 500
			}
		]
	}`)

	msg, err := DecodeResponse(wfFrame)
	if err != nil {
		t.Fatalf("DecodeResponse for workflow aggregate failed: %v", err)
	}
	wfResp, ok := msg.(*GetWorkflowAggregatesResponse)
	if !ok {
		t.Fatalf("expected *GetWorkflowAggregatesResponse, got %T", msg)
	}
	if len(wfResp.Output) != 1 {
		t.Fatalf("expected 1 output row, got %d", len(wfResp.Output))
	}
	row := wfResp.Output[0]
	if row.Count == nil || *row.Count != 42 {
		t.Errorf("expected count 42, got %v", row.Count)
	}
	if row.Group["status"] == nil || *row.Group["status"] != "SUCCESS" {
		t.Errorf("expected group status SUCCESS, got %v", row.Group["status"])
	}
	if row.MinCreatedAt == nil || *row.MinCreatedAt != 1756713600000 {
		t.Errorf("expected min_created_at 1756713600000, got %v", row.MinCreatedAt)
	}
	if row.MaxQueueWaitMs == nil || *row.MaxQueueWaitMs != 150 {
		t.Errorf("expected max_queue_wait_ms 150, got %v", row.MaxQueueWaitMs)
	}
	if row.MaxTotalLatencyMs == nil || *row.MaxTotalLatencyMs != 500 {
		t.Errorf("expected max_total_latency_ms 500, got %v", row.MaxTotalLatencyMs)
	}

	stepFrame := []byte(`{
		"type": "get_step_aggregates",
		"request_id": "req-agg-2",
		"output": [
			{
				"group": {"function_name": "processStep"},
				"count": 10,
				"max_duration_ms": 120
			}
		]
	}`)

	stepMsg, err := DecodeResponse(stepFrame)
	if err != nil {
		t.Fatalf("DecodeResponse for step aggregate failed: %v", err)
	}
	stepResp, ok := stepMsg.(*GetStepAggregatesResponse)
	if !ok {
		t.Fatalf("expected *GetStepAggregatesResponse, got %T", stepMsg)
	}
	if len(stepResp.Output) != 1 {
		t.Fatalf("expected 1 output row, got %d", len(stepResp.Output))
	}
	sRow := stepResp.Output[0]
	if sRow.Count == nil || *sRow.Count != 10 {
		t.Errorf("expected count 10, got %v", sRow.Count)
	}
	if sRow.MaxDurationMs == nil || *sRow.MaxDurationMs != 120 {
		t.Errorf("expected max_duration_ms 120, got %v", sRow.MaxDurationMs)
	}
	if sRow.Group["function_name"] == nil || *sRow.Group["function_name"] != "processStep" {
		t.Errorf("expected group function_name processStep, got %v", sRow.Group["function_name"])
	}
}
