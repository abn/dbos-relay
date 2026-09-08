package dataplane_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"
	"github.com/google/uuid"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/protocol"
)

func liveWorkflow(ctx dbos.Context, name string) (string, error) {
	return "hello " + name, nil
}

func TestLiveDatabase_SDKClientDataPlane(t *testing.T) {
	dbURL := os.Getenv("RELAY_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	appName := "dataplane-live-app"

	// 1. Initialize real DBOS context to run migrations and register workflow
	dbosCtx, err := dbos.NewContext(ctx, dbos.Config{
		AppName:     appName,
		DatabaseURL: dbURL,
	})
	if err != nil {
		t.Fatalf("failed to initialize live DBOS context: %v", err)
	}

	// Register test workflow function
	wfName := "liveTestWorkflow"
	dbos.RegisterWorkflow(dbosCtx, liveWorkflow, dbos.WithWorkflowName(wfName))

	// Launch DBOS executor
	if err := dbos.Launch(dbosCtx); err != nil {
		t.Fatalf("failed to launch DBOS context: %v", err)
	}

	// Execute workflow to seed real system database records
	handle, err := dbos.RunWorkflow(dbosCtx, liveWorkflow, "world")
	if err != nil {
		t.Fatalf("failed to run live workflow: %v", err)
	}
	wfID := handle.GetWorkflowID()

	// 2. Initialize Relay SDKClient pointing to the same system database
	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		DatabaseURL:      dbURL,
		ApplicationName:  appName,
		StatementTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create SDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	// 3. Test GetWorkflow
	getRes, err := client.Dispatch(ctx, &protocol.GetWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflow,
			RequestID: uuid.NewString(),
		},
		WorkflowID: wfID,
	})
	if err != nil {
		t.Fatalf("SDKClient.Dispatch(GetWorkflow) failed: %v", err)
	}
	getResp, ok := getRes.(*protocol.GetWorkflowResponse)
	if !ok || getResp.Output == nil {
		t.Fatalf("expected *protocol.GetWorkflowResponse with output, got %+v", getRes)
	}
	if getResp.Output.WorkflowUUID != wfID {
		t.Errorf("expected workflow UUID %s, got %s", wfID, getResp.Output.WorkflowUUID)
	}
	if getResp.Output.Status == nil || *getResp.Output.Status != "SUCCESS" {
		t.Errorf("expected status SUCCESS, got %v", getResp.Output.Status)
	}

	// 4. Test ListWorkflows
	listRes, err := client.Dispatch(ctx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListWorkflows,
			RequestID: uuid.NewString(),
		},
		Body: protocol.ListWorkflowsRequestBody{
			WorkflowUUIDs: []string{wfID},
		},
	})
	if err != nil {
		t.Fatalf("SDKClient.Dispatch(ListWorkflows) failed: %v", err)
	}
	listResp, ok := listRes.(*protocol.ListWorkflowsResponse)
	if !ok || len(listResp.Output) == 0 {
		t.Fatalf("expected non-empty *protocol.ListWorkflowsResponse, got %+v", listRes)
	}
	if listResp.Output[0].WorkflowUUID != wfID {
		t.Errorf("expected listed workflow UUID %s, got %s", wfID, listResp.Output[0].WorkflowUUID)
	}

	// 5. Test ListSteps
	stepsRes, err := client.Dispatch(ctx, &protocol.ListStepsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListSteps,
			RequestID: uuid.NewString(),
		},
		WorkflowID: wfID,
	})
	if err != nil {
		t.Fatalf("SDKClient.Dispatch(ListSteps) failed: %v", err)
	}
	stepsResp, ok := stepsRes.(*protocol.ListStepsResponse)
	if !ok {
		t.Fatalf("expected *protocol.ListStepsResponse, got %+v", stepsRes)
	}
	if stepsResp.Output == nil {
		t.Errorf("expected non-nil steps output")
	}

	// 6. Test ForkWorkflow via SDKClient
	forkRes, err := client.Dispatch(ctx, &protocol.ForkWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeForkWorkflow,
			RequestID: uuid.NewString(),
		},
		Body: protocol.ForkWorkflowRequestBody{
			WorkflowID: wfID,
			StartStep:  0,
		},
	})
	if err != nil {
		t.Fatalf("SDKClient.Dispatch(ForkWorkflow) failed: %v", err)
	}
	forkResp, ok := forkRes.(*protocol.ForkWorkflowResponse)
	if !ok || forkResp.NewWorkflowID == nil {
		t.Fatalf("expected *protocol.ForkWorkflowResponse with NewWorkflowID, got %+v", forkRes)
	}
	forkedID := *forkResp.NewWorkflowID

	// 7. Test CancelWorkflow on the forked enqueued workflow
	cancelRes, err := client.Dispatch(ctx, &protocol.CancelWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeCancel,
			RequestID: uuid.NewString(),
		},
		WorkflowID: forkedID,
	})
	if err != nil {
		t.Fatalf("SDKClient.Dispatch(CancelWorkflow) failed: %v", err)
	}
	cancelResp, ok := cancelRes.(*protocol.CancelWorkflowResponse)
	if !ok || !cancelResp.Success {
		t.Fatalf("expected CancelWorkflowResponse success, got %+v", cancelRes)
	}

	// Verify status flipped to CANCELLED
	chkRes, err := client.Dispatch(ctx, &protocol.GetWorkflowRequest{
		Envelope:   protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: uuid.NewString()},
		WorkflowID: forkedID,
	})
	if err != nil {
		t.Fatalf("failed to query cancelled workflow: %v", err)
	}
	chkResp := chkRes.(*protocol.GetWorkflowResponse)
	if chkResp.Output.Status == nil || *chkResp.Output.Status != "CANCELLED" {
		t.Errorf("expected status CANCELLED, got %v", chkResp.Output.Status)
	}

	// 8. Test ResumeWorkflow on the cancelled workflow
	resumeRes, err := client.Dispatch(ctx, &protocol.ResumeWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeResume,
			RequestID: uuid.NewString(),
		},
		WorkflowID: forkedID,
	})
	if err != nil {
		t.Fatalf("SDKClient.Dispatch(ResumeWorkflow) failed: %v", err)
	}
	resumeResp, ok := resumeRes.(*protocol.ResumeWorkflowResponse)
	if !ok || !resumeResp.Success {
		t.Fatalf("expected ResumeWorkflowResponse success, got %+v", resumeRes)
	}

	// Verify status flipped to ENQUEUED
	chkRes2, err := client.Dispatch(ctx, &protocol.GetWorkflowRequest{
		Envelope:   protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: uuid.NewString()},
		WorkflowID: forkedID,
	})
	if err != nil {
		t.Fatalf("failed to query resumed workflow: %v", err)
	}
	chkResp2 := chkRes2.(*protocol.GetWorkflowResponse)
	if chkResp2.Output.Status == nil || *chkResp2.Output.Status != "ENQUEUED" {
		t.Errorf("expected status ENQUEUED, got %v", chkResp2.Output.Status)
	}
}
