package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
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

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("Failed to read golden file: %v", err)
			}

			// We need to guess request or response for legacy tests.
			msg, err := DecodeRequest(data)
			if err != nil {
				msg, err = DecodeResponse(data)
				if err != nil {
					t.Fatalf("Decode failed: %v", err)
				}
			}

			// Verify it implements Message
			if msg.GetMessageType() == "" || msg.GetRequestID() == "" {
				t.Errorf("Message interface not fully populated")
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

			// Misclassification check using old logic fallback just to prove it doesn't fail DecodeResponse explicitly
			_, err = DecodeResponse(encoded)
			if err != nil {
				t.Fatalf("DecodeResponse failed for zero-value %T: %v", msg, err)
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
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
