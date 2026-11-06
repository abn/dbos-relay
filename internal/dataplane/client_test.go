package dataplane

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	dbos "github.com/dbos-inc/dbos-transact-golang/dbos"

	"github.com/abn/relay/internal/protocol"
)

func TestNonDatabaseReason(t *testing.T) {
	executorOnly := []protocol.MessageType{
		protocol.MessageTypeGetWorkflowEvents,
		protocol.MessageTypeGetWorkflowNotifications,
		protocol.MessageTypeGetWorkflowStreams,
		protocol.MessageTypeExportWorkflow,
		protocol.MessageTypeImportWorkflow,
	}
	for _, messageType := range executorOnly {
		if reason := NonDatabaseReason(messageType); reason == "" {
			t.Errorf("expected a reason for %s", messageType)
		}
	}
	databaseBacked := []protocol.MessageType{
		protocol.MessageTypeGetMetrics,
		protocol.MessageTypeRetention,
		protocol.MessageTypeListWorkflows,
	}
	for _, messageType := range databaseBacked {
		if reason := NonDatabaseReason(messageType); reason != "" {
			t.Errorf("expected no executor-only reason for %s, got %q", messageType, reason)
		}
	}
}

// TestListWorkflowsRequestBody_FieldCoverage guards against request filter fields
// that are neither applied by the local data plane nor knowingly forwarded without
// local application.
func TestListWorkflowsRequestBody_FieldCoverage(t *testing.T) {
	expectedMappedFields := map[string]bool{
		"WorkflowUUIDs":      true,
		"WorkflowName":       true,
		"AuthenticatedUser":  true,
		"StartTime":          true,
		"EndTime":            true,
		"CompletedAfter":     true,
		"CompletedBefore":    true,
		"DequeuedAfter":      true,
		"DequeuedBefore":     true,
		"Status":             true,
		"ApplicationVersion": true,
		"ForkedFrom":         true,
		"ParentWorkflowID":   true,
		"WasForkedFrom":      true,
		"HasParent":          true,
		"IsFork":             true,
		"QueueName":          true,
		"Limit":              true,
		"Offset":             true,
		"SortDesc":           true,
		"WorkflowIDPrefix":   true,
		"LoadInput":          true,
		"LoadOutput":         true,
		"ExecutorID":         true,
		"QueuesOnly":         true,
		"Attributes":         true,
		"ScheduleName":       true,
		"ApplicationName":    true,
	}

	typ := reflect.TypeOf(protocol.ListWorkflowsRequestBody{})
	if got := typ.NumField(); got != len(expectedMappedFields) {
		t.Fatalf("ListWorkflowsRequestBody field count mismatch: struct has %d fields, expected %d mapped", got, len(expectedMappedFields))
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if expectedMappedFields[field.Name] {
			continue
		}
		t.Errorf("ListWorkflowsRequestBody field %q is not mapped by the data plane", field.Name)
	}
}

// TestMapWorkflowStatus_FieldParity tests that all expressible fields from dbos.WorkflowStatus
// are populated on protocol.ListWorkflowsResponseBody with exact upstream parity.
func TestMapWorkflowStatus_FieldParity(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	timeout := 30 * time.Second
	deadline := now.Add(1 * time.Minute)
	startedAt := now.Add(-5 * time.Second)
	completedAt := now
	delayUntil := now.Add(10 * time.Second)

	rawInput := map[string]any{"arg": "value"}
	rawOutput := map[string]any{"result": "success"}
	rawInputBytes, _ := json.Marshal(rawInput)
	rawOutputBytes, _ := json.Marshal(rawOutput)

	status := dbos.WorkflowStatus{
		ID:                 "wf-test-123",
		Status:             dbos.WorkflowStatusPending,
		Name:               "TestWorkflow",
		AuthenticatedUser:  "testuser",
		AssumedRole:        "admin",
		AuthenticatedRoles: []string{"admin", "operator"},
		ApplicationVersion: "v1.2.3",
		ExecutorID:         "exec-456",
		CreatedAt:          now.Add(-10 * time.Second),
		UpdatedAt:          now,
		QueueName:          "priority-queue",
		ForkedFrom:         "wf-parent-000",
		WasForkedFrom:      false,
		ParentWorkflowID:   "wf-root-999",
		CompletedAt:        completedAt,
		Timeout:            timeout,
		Deadline:           deadline,
		StartedAt:          startedAt,
		DeduplicationID:    "dedup-789",
		Priority:           0,
		QueuePartitionKey:  "part-key-1",
		DelayUntil:         delayUntil,
		Attributes:         map[string]any{"tier": "gold"},
		ScheduleName:       "hourly-sync",
		ApplicationName:    "order-service",
		Input:              rawInputBytes,
		Output:             rawOutputBytes,
		Error:              errors.New("simulated error"),
	}

	mapped := mapWorkflowStatus(status)

	// Verify specific parity invariants
	if mapped.WorkflowUUID != "wf-test-123" {
		t.Errorf("expected WorkflowUUID wf-test-123, got %s", mapped.WorkflowUUID)
	}
	if mapped.Priority == nil || *mapped.Priority != "0" {
		t.Errorf("expected Priority unconditionally set to \"0\", got %v", mapped.Priority)
	}
	if mapped.WasForkedFrom == nil || *mapped.WasForkedFrom != false {
		t.Errorf("expected WasForkedFrom unconditionally set to false, got %v", mapped.WasForkedFrom)
	}
	if mapped.DequeuedAt == nil {
		t.Error("expected DequeuedAt to be set when status is PENDING with non-zero StartedAt")
	} else {
		expectedDequeued := strconv.FormatInt(startedAt.UnixMilli(), 10)
		if *mapped.DequeuedAt != expectedDequeued {
			t.Errorf("expected DequeuedAt %s, got %s", expectedDequeued, *mapped.DequeuedAt)
		}
	}

	// For non-pending workflow, DequeuedAt should not be set
	statusSuccess := status
	statusSuccess.Status = dbos.WorkflowStatusSuccess
	mappedSuccess := mapWorkflowStatus(statusSuccess)
	if mappedSuccess.DequeuedAt != nil {
		t.Errorf("expected DequeuedAt to be nil for SUCCESS status, got %s", *mappedSuccess.DequeuedAt)
	}

	// Reflection check: assert all 28 expressible fields are non-nil
	unmappedAllowed := map[string]bool{
		"WorkflowClassName":  true,
		"WorkflowConfigName": true,
	}

	val := reflect.ValueOf(mapped)
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		fieldName := typ.Field(i).Name
		if unmappedAllowed[fieldName] {
			continue
		}
		fieldVal := val.Field(i)
		if fieldName == "WorkflowUUID" {
			if fieldVal.String() == "" {
				t.Errorf("expected non-empty WorkflowUUID")
			}
			continue
		}
		if fieldVal.Kind() == reflect.Pointer && fieldVal.IsNil() {
			t.Errorf("expected field %q to be populated, but was nil", fieldName)
		}
	}
}

func TestWriteInputValidation(t *testing.T) {
	if err := (EnqueueWorkflowInput{WorkflowName: "wf", QueueName: "q", Payload: []byte(`{"a":1}`)}).Validate(); err != nil {
		t.Fatalf("expected valid enqueue input, got %v", err)
	}
	if err := (EnqueueWorkflowInput{WorkflowName: "", QueueName: "q", Payload: []byte(`{}`)}).Validate(); err == nil {
		t.Fatal("expected missing workflow name to fail")
	}
	if err := (EnqueueWorkflowInput{WorkflowName: "wf", QueueName: "", Payload: []byte(`{}`)}).Validate(); err == nil {
		t.Fatal("expected missing queue name to fail")
	}
	if err := (EnqueueWorkflowInput{WorkflowName: "wf", QueueName: "q", Payload: []byte(`not-json`)}).Validate(); err == nil {
		t.Fatal("expected invalid JSON payload to fail")
	}
	if err := (EnqueueWorkflowInput{WorkflowName: "wf", QueueName: "q"}).Validate(); err == nil {
		t.Fatal("expected empty payload to fail")
	}

	if err := (SendWorkflowMessageInput{DestinationID: "wf-1", Payload: []byte(`"opaque"`)}).Validate(); err != nil {
		t.Fatalf("expected valid send input, got %v", err)
	}
	if err := (SendWorkflowMessageInput{DestinationID: "", Payload: []byte(`"x"`)}).Validate(); err == nil {
		t.Fatal("expected missing destination to fail")
	}
	if err := (SendWorkflowMessageInput{DestinationID: "wf-1", Payload: []byte(`not-json`)}).Validate(); err == nil {
		t.Fatal("expected invalid JSON payload to fail")
	}
}
