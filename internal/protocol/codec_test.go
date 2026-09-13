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

func TestDecodeEncodeRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		msg        Message
		isResponse bool
	}{
		{"ExecutorInfoRequest", &ExecutorInfoRequest{Envelope: Envelope{Type: MessageTypeExecutorInfo, RequestID: "r1"}}, false},
		{"ExecutorInfoResponse", &ExecutorInfoResponse{Envelope: Envelope{Type: MessageTypeExecutorInfo, RequestID: "r1"}, ExecutorID: "e1"}, true},
		{"RecoveryRequest", &RecoveryRequest{Envelope: Envelope{Type: MessageTypeRecovery, RequestID: "r2"}, ExecutorIDs: []string{"e1"}}, false},
		{"RecoveryResponse", &RecoveryResponse{Envelope: Envelope{Type: MessageTypeRecovery, RequestID: "r2"}, Success: true}, true},
		{"ListWorkflowsRequest", &ListWorkflowsRequest{Envelope: Envelope{Type: MessageTypeListWorkflows, RequestID: "r3"}, Body: ListWorkflowsRequestBody{Limit: intPtr(10)}}, false},
		{"ListWorkflowsResponse", &ListWorkflowsResponse{Envelope: Envelope{Type: MessageTypeListWorkflows, RequestID: "r3"}, Output: []ListWorkflowsResponseBody{{WorkflowUUID: "wf-1"}}}, true},
		{"GetWorkflowRequest", &GetWorkflowRequest{Envelope: Envelope{Type: MessageTypeGetWorkflow, RequestID: "r4"}, WorkflowID: "wf-1"}, false},
		{"GetWorkflowResponse", &GetWorkflowResponse{Envelope: Envelope{Type: MessageTypeGetWorkflow, RequestID: "r4"}, Output: &ListWorkflowsResponseBody{WorkflowUUID: "wf-1"}}, true},
		{"AlertRequest", &AlertRequest{Envelope: Envelope{Type: MessageTypeAlert, RequestID: "r5"}, Name: "alert"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := Encode(tt.msg)
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

	expectedMap := map[string]struct {
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
