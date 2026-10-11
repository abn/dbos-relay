package dataplane

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	dbos "github.com/dbos-inc/dbos-transact-golang/dbos"

	"github.com/abn/relay/internal/protocol"
)

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

	// Fields forwarded to executors on the wire but not applied by the local
	// SDK-client fallback. The pinned DBOS Go SDK (v1.3.0) predates
	// WithFilterIsFork (v1.6.0), so this fallback returns a superset.
	knownUnmappedFields := map[string]bool{
		"IsFork": true,
	}

	typ := reflect.TypeOf(protocol.ListWorkflowsRequestBody{})
	if got := typ.NumField(); got != len(expectedMappedFields)+len(knownUnmappedFields) {
		t.Fatalf("ListWorkflowsRequestBody field count mismatch: struct has %d fields, expected %d mapped and %d knowingly unmapped", got, len(expectedMappedFields), len(knownUnmappedFields))
	}

	// A knowingly unmapped field must genuinely stay unapplied. When the SDK pin
	// moves and the data plane starts applying one, this fails until the coverage
	// sets are updated, so the guard cannot go stale in that direction.
	knownUnmappedSDKSymbols := map[string]string{
		"IsFork": "dbos.WithFilterIsFork",
	}
	for name, symbol := range knownUnmappedSDKSymbols {
		if !knownUnmappedFields[name] {
			continue
		}
		source, err := os.ReadFile("client.go")
		if err != nil {
			t.Fatalf("reading client.go: %v", err)
		}
		if bytes.Contains(source, []byte(symbol)) {
			t.Errorf("ListWorkflowsRequestBody field %q is listed as knowingly unmapped but client.go applies %s; map it in the data plane and drop the entry", name, symbol)
		}
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if expectedMappedFields[field.Name] || knownUnmappedFields[field.Name] {
			continue
		}
		t.Errorf("ListWorkflowsRequestBody field %q is neither mapped by the data plane nor knowingly unmapped", field.Name)
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
