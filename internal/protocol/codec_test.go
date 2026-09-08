package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeEncodeRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
	}{
		{"ExecutorInfoRequest", &ExecutorInfoRequest{Envelope: Envelope{Type: MessageTypeExecutorInfo, RequestID: "r1"}}},
		{"ExecutorInfoResponse", &ExecutorInfoResponse{Envelope: Envelope{Type: MessageTypeExecutorInfo, RequestID: "r1"}, ExecutorID: "e1"}},
		{"RecoveryRequest", &RecoveryRequest{Envelope: Envelope{Type: MessageTypeRecovery, RequestID: "r2"}, ExecutorIDs: []string{"e1"}}},
		{"RecoveryResponse", &RecoveryResponse{Envelope: Envelope{Type: MessageTypeRecovery, RequestID: "r2"}, Success: true}},
		{"ListWorkflowsRequest", &ListWorkflowsRequest{Envelope: Envelope{Type: MessageTypeListWorkflows, RequestID: "r3"}, Body: ListWorkflowsRequestBody{Limit: intPtr(10)}}},
		{"ListWorkflowsResponse", &ListWorkflowsResponse{Envelope: Envelope{Type: MessageTypeListWorkflows, RequestID: "r3"}, Output: []ListWorkflowsResponseBody{{WorkflowUUID: "wf-1"}}}},
		{"GetWorkflowRequest", &GetWorkflowRequest{Envelope: Envelope{Type: MessageTypeGetWorkflow, RequestID: "r4"}, WorkflowID: "wf-1"}},
		{"GetWorkflowResponse", &GetWorkflowResponse{Envelope: Envelope{Type: MessageTypeGetWorkflow, RequestID: "r4"}, Output: &ListWorkflowsResponseBody{WorkflowUUID: "wf-1"}}},
		{"AlertRequest", &AlertRequest{Envelope: Envelope{Type: MessageTypeAlert, RequestID: "r5"}, Name: "alert"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := Encode(tt.msg)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}
			decoded, err := Decode(encoded)
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

			msg, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
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
	msg, err := Decode(data)
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
	_, err := Decode([]byte(`{"request_id":"1"}`))
	if err == nil {
		t.Errorf("Expected error for missing type")
	}

	// Invalid JSON
	_, err = Decode([]byte(`{foo}`))
	if err == nil {
		t.Errorf("Expected error for invalid JSON")
	}

	// Unknown type
	_, err = Decode([]byte(`{"type":"unknown_type", "request_id":"1"}`))
	if err == nil {
		t.Errorf("Expected error for unknown type")
	}
}

func intPtr(i int) *int {
	return &i
}
