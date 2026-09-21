package dataplane_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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
	// Wait for completion: RunWorkflow only hands back a handle, so
	// asserting SUCCESS below would otherwise race the workflow.
	if _, err := handle.GetResult(); err != nil {
		t.Fatalf("live workflow failed: %v", err)
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

func liveSuccessTask(ctx dbos.Context, name string) (string, error) {
	return "done: " + name, nil
}

func liveErrorTask(ctx dbos.Context, name string) (string, error) {
	return "", errors.New("intentional task failure")
}

var (
	liveFilterBlockCh   = make(chan struct{})
	liveFilterStartedCh = make(chan struct{}, 1)
)

func liveFilterPendingTask(ctx dbos.Context, name string) (string, error) {
	select {
	case liveFilterStartedCh <- struct{}{}:
	default:
	}
	<-liveFilterBlockCh
	return "done: " + name, nil
}

func TestLiveDatabase_FilterParity(t *testing.T) {
	dbURL := os.Getenv("RELAY_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	appName := fmt.Sprintf("dataplane-filter-app-%d", time.Now().UnixNano())

	dbosCtx, err := dbos.NewContext(ctx, dbos.Config{
		AppName:     appName,
		DatabaseURL: dbURL,
	})
	if err != nil {
		t.Fatalf("failed to initialize live DBOS context: %v", err)
	}

	dbos.RegisterWorkflow(dbosCtx, liveSuccessTask, dbos.WithWorkflowName("successTask"))
	dbos.RegisterWorkflow(dbosCtx, liveErrorTask, dbos.WithWorkflowName("errorTask"))
	dbos.RegisterWorkflow(dbosCtx, liveFilterPendingTask, dbos.WithWorkflowName("pendingTask"))

	if err := dbos.Launch(dbosCtx); err != nil {
		t.Fatalf("failed to launch DBOS context: %v", err)
	}

	// 1. Seed 3 SUCCESS workflows
	h1, err := dbos.RunWorkflow(dbosCtx, liveSuccessTask, "success-1")
	if err != nil {
		t.Fatalf("failed to run success-1: %v", err)
	}
	h2, err := dbos.RunWorkflow(dbosCtx, liveSuccessTask, "success-2")
	if err != nil {
		t.Fatalf("failed to run success-2: %v", err)
	}
	h3, err := dbos.RunWorkflow(dbosCtx, liveSuccessTask, "success-3")
	if err != nil {
		t.Fatalf("failed to run success-3: %v", err)
	}
	if _, err := h1.GetResult(); err != nil {
		t.Fatalf("h1 failed: %v", err)
	}
	if _, err := h2.GetResult(); err != nil {
		t.Fatalf("h2 failed: %v", err)
	}
	if _, err := h3.GetResult(); err != nil {
		t.Fatalf("h3 failed: %v", err)
	}
	id1, id2, id3 := h1.GetWorkflowID(), h2.GetWorkflowID(), h3.GetWorkflowID()

	// 2. Seed 1 ERROR workflow
	he, err := dbos.RunWorkflow(dbosCtx, liveErrorTask, "error-1")
	if err != nil {
		t.Fatalf("failed to run error-1: %v", err)
	}
	_, _ = he.GetResult()
	idE := he.GetWorkflowID()

	// 3. Seed 1 PENDING workflow
	hp, err := dbos.RunWorkflow(dbosCtx, liveFilterPendingTask, "pending-1")
	if err != nil {
		t.Fatalf("failed to run pending-1: %v", err)
	}
	select {
	case <-liveFilterStartedCh:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for pending workflow to start")
	}
	idP := hp.GetWorkflowID()
	defer func() {
		close(liveFilterBlockCh)
		_, _ = hp.GetResult()
	}()

	// Initialize Relay SDKClient
	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		DatabaseURL:      dbURL,
		ApplicationName:  appName,
		StatementTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create SDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Assert Filter by status=PENDING: returns pending workflow and 0 of the 3 SUCCESS rows
	resPending, err := client.Dispatch(ctx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: uuid.NewString()},
		Body: protocol.ListWorkflowsRequestBody{
			ApplicationName: []string{appName},
			Status:          []string{"PENDING"},
		},
	})
	if err != nil {
		t.Fatalf("ListWorkflows(Status=PENDING) failed: %v", err)
	}
	respPending := resPending.(*protocol.ListWorkflowsResponse)
	if len(respPending.Output) != 1 {
		t.Fatalf("expected 1 PENDING workflow, got %d", len(respPending.Output))
	}
	if respPending.Output[0].WorkflowUUID != idP {
		t.Errorf("expected workflow UUID %s, got %s", idP, respPending.Output[0].WorkflowUUID)
	}
	for _, out := range respPending.Output {
		if out.WorkflowUUID == id1 || out.WorkflowUUID == id2 || out.WorkflowUUID == id3 {
			t.Errorf("PENDING filter leaked SUCCESS workflow %s", out.WorkflowUUID)
		}
	}

	// Assert Filter by status=SUCCESS
	resSuccess, err := client.Dispatch(ctx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: uuid.NewString()},
		Body: protocol.ListWorkflowsRequestBody{
			ApplicationName: []string{appName},
			Status:          []string{"SUCCESS"},
		},
	})
	if err != nil {
		t.Fatalf("ListWorkflows(Status=SUCCESS) failed: %v", err)
	}
	respSuccess := resSuccess.(*protocol.ListWorkflowsResponse)
	if len(respSuccess.Output) != 3 {
		t.Fatalf("expected 3 SUCCESS workflows, got %d", len(respSuccess.Output))
	}

	// Assert Filter by status=ERROR
	resError, err := client.Dispatch(ctx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: uuid.NewString()},
		Body: protocol.ListWorkflowsRequestBody{
			ApplicationName: []string{appName},
			Status:          []string{"ERROR"},
		},
	})
	if err != nil {
		t.Fatalf("ListWorkflows(Status=ERROR) failed: %v", err)
	}
	respError := resError.(*protocol.ListWorkflowsResponse)
	if len(respError.Output) != 1 {
		t.Fatalf("expected 1 ERROR workflow, got %d", len(respError.Output))
	}
	if respError.Output[0].WorkflowUUID != idE {
		t.Errorf("expected ERROR workflow UUID %s, got %s", idE, respError.Output[0].WorkflowUUID)
	}

	// Assert Filter by WorkflowName
	resName, err := client.Dispatch(ctx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: uuid.NewString()},
		Body: protocol.ListWorkflowsRequestBody{
			ApplicationName: []string{appName},
			WorkflowName:    []string{"successTask"},
		},
	})
	if err != nil {
		t.Fatalf("ListWorkflows(WorkflowName) failed: %v", err)
	}
	respName := resName.(*protocol.ListWorkflowsResponse)
	if len(respName.Output) != 3 {
		t.Fatalf("expected 3 workflows for successTask, got %d", len(respName.Output))
	}

	// Assert Limit and Offset
	limit := 2
	offset := 1
	resLimit, err := client.Dispatch(ctx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: uuid.NewString()},
		Body: protocol.ListWorkflowsRequestBody{
			ApplicationName: []string{appName},
			Limit:           &limit,
			Offset:          &offset,
		},
	})
	if err != nil {
		t.Fatalf("ListWorkflows(Limit, Offset) failed: %v", err)
	}
	respLimit := resLimit.(*protocol.ListWorkflowsResponse)
	if len(respLimit.Output) != 2 {
		t.Fatalf("expected 2 workflows for limit=2, got %d", len(respLimit.Output))
	}

	// Assert SortDesc ordering
	resSort, err := client.Dispatch(ctx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: uuid.NewString()},
		Body: protocol.ListWorkflowsRequestBody{
			ApplicationName: []string{appName},
			SortDesc:        true,
		},
	})
	if err != nil {
		t.Fatalf("ListWorkflows(SortDesc) failed: %v", err)
	}
	respSort := resSort.(*protocol.ListWorkflowsResponse)
	if len(respSort.Output) < 5 {
		t.Fatalf("expected at least 5 workflows, got %d", len(respSort.Output))
	}
	for i := 0; i < len(respSort.Output)-1; i++ {
		t1 := respSort.Output[i].CreatedAt
		t2 := respSort.Output[i+1].CreatedAt
		if t1 != nil && t2 != nil && *t1 < *t2 {
			t.Errorf("expected descending sort, but item %d is before item %d", i, i+1)
		}
	}

	// Assert Filter by WorkflowUUIDs
	resIDs, err := client.Dispatch(ctx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: uuid.NewString()},
		Body: protocol.ListWorkflowsRequestBody{
			WorkflowUUIDs: []string{id1, id2},
		},
	})
	if err != nil {
		t.Fatalf("ListWorkflows(WorkflowUUIDs) failed: %v", err)
	}
	respIDs := resIDs.(*protocol.ListWorkflowsResponse)
	if len(respIDs.Output) != 2 {
		t.Fatalf("expected 2 workflows for UUID list, got %d", len(respIDs.Output))
	}
}

func TestLiveDatabase_ContextCancellationAndTimeout(t *testing.T) {
	dbURL := os.Getenv("RELAY_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	appName := fmt.Sprintf("dataplane-timeout-app-%d", time.Now().UnixNano())

	// Create SDKClient with 5s statement timeout
	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		DatabaseURL:      dbURL,
		ApplicationName:  appName,
		StatementTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create SDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Verify cancelled context returns context.Canceled
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = client.Dispatch(cancelCtx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: uuid.NewString()},
		Body:     protocol.ListWorkflowsRequestBody{},
	})
	if err == nil {
		t.Fatalf("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestLiveDatabase_BulkOperations(t *testing.T) {
	dbURL := os.Getenv("RELAY_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	appName := fmt.Sprintf("dataplane-bulk-app-%d", time.Now().UnixNano())

	// Initialize DBOS context and launch
	dbosCtx, err := dbos.NewContext(ctx, dbos.Config{
		AppName:     appName,
		DatabaseURL: dbURL,
	})
	if err != nil {
		t.Fatalf("failed to initialize live DBOS context: %v", err)
	}

	dbos.RegisterWorkflow(dbosCtx, liveSuccessTask, dbos.WithWorkflowName("bulkSeedWorkflow"))

	if err := dbos.Launch(dbosCtx); err != nil {
		t.Fatalf("failed to launch DBOS context: %v", err)
	}

	// Seed an initial workflow to fork from
	seedH, err := dbos.RunWorkflow(dbosCtx, liveSuccessTask, "seed")
	if err != nil {
		t.Fatalf("failed to run seed workflow: %v", err)
	}
	if _, err := seedH.GetResult(); err != nil {
		t.Fatalf("seed workflow failed: %v", err)
	}
	seedID := seedH.GetWorkflowID()

	// Initialize Relay SDKClient
	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		DatabaseURL:      dbURL,
		ApplicationName:  appName,
		StatementTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create SDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Fork two workflows
	fork1, err := client.Dispatch(ctx, &protocol.ForkWorkflowRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeForkWorkflow, RequestID: uuid.NewString()},
		Body:     protocol.ForkWorkflowRequestBody{WorkflowID: seedID, StartStep: 0},
	})
	if err != nil {
		t.Fatalf("fork 1 failed: %v", err)
	}
	id1 := *fork1.(*protocol.ForkWorkflowResponse).NewWorkflowID

	fork2, err := client.Dispatch(ctx, &protocol.ForkWorkflowRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeForkWorkflow, RequestID: uuid.NewString()},
		Body:     protocol.ForkWorkflowRequestBody{WorkflowID: seedID, StartStep: 0},
	})
	if err != nil {
		t.Fatalf("fork 2 failed: %v", err)
	}
	id2 := *fork2.(*protocol.ForkWorkflowResponse).NewWorkflowID

	// 1. Bulk Cancel
	cancelRes, err := client.Dispatch(ctx, &protocol.CancelWorkflowRequest{
		Envelope:       protocol.Envelope{Type: protocol.MessageTypeCancel, RequestID: uuid.NewString()},
		WorkflowIDs:    []string{id1, id2},
		CancelChildren: true,
	})
	if err != nil {
		t.Fatalf("bulk cancel failed: %v", err)
	}
	if !cancelRes.(*protocol.CancelWorkflowResponse).Success {
		t.Fatalf("bulk cancel returned success=false")
	}

	// Verify both workflows are CANCELLED
	for _, id := range []string{id1, id2} {
		chk, err := client.Dispatch(ctx, &protocol.GetWorkflowRequest{
			Envelope:   protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: uuid.NewString()},
			WorkflowID: id,
		})
		if err != nil {
			t.Fatalf("failed to get workflow %s: %v", id, err)
		}
		status := chk.(*protocol.GetWorkflowResponse).Output.Status
		if status == nil || *status != "CANCELLED" {
			t.Errorf("expected status CANCELLED for %s, got %v", id, status)
		}
	}

	// 2. Bulk Resume
	resumeRes, err := client.Dispatch(ctx, &protocol.ResumeWorkflowRequest{
		Envelope:    protocol.Envelope{Type: protocol.MessageTypeResume, RequestID: uuid.NewString()},
		WorkflowIDs: []string{id1, id2},
	})
	if err != nil {
		t.Fatalf("bulk resume failed: %v", err)
	}
	if !resumeRes.(*protocol.ResumeWorkflowResponse).Success {
		t.Fatalf("bulk resume returned success=false")
	}

	// Verify both workflows are ENQUEUED
	for _, id := range []string{id1, id2} {
		chk, err := client.Dispatch(ctx, &protocol.GetWorkflowRequest{
			Envelope:   protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: uuid.NewString()},
			WorkflowID: id,
		})
		if err != nil {
			t.Fatalf("failed to get workflow %s: %v", id, err)
		}
		status := chk.(*protocol.GetWorkflowResponse).Output.Status
		if status == nil || *status != "ENQUEUED" {
			t.Errorf("expected status ENQUEUED for %s, got %v", id, status)
		}
	}
}

func TestLiveDatabase_AggregatesDispatch(t *testing.T) {
	dbURL := os.Getenv("RELAY_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	appName := fmt.Sprintf("dataplane-agg-app-%d", time.Now().UnixNano())

	// Initialize DBOS context and launch
	dbosCtx, err := dbos.NewContext(ctx, dbos.Config{
		AppName:     appName,
		DatabaseURL: dbURL,
	})
	if err != nil {
		t.Fatalf("failed to initialize live DBOS context: %v", err)
	}

	dbos.RegisterWorkflow(dbosCtx, liveSuccessTask, dbos.WithWorkflowName("aggWorkflow"))

	if err := dbos.Launch(dbosCtx); err != nil {
		t.Fatalf("failed to launch DBOS context: %v", err)
	}

	// Seed one workflow
	handle, err := dbos.RunWorkflow(dbosCtx, liveSuccessTask, "agg-test")
	if err != nil {
		t.Fatalf("failed to run workflow: %v", err)
	}
	if _, err := handle.GetResult(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	// Initialize Relay SDKClient
	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		DatabaseURL:      dbURL,
		ApplicationName:  appName,
		StatementTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create SDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	// 1. GetWorkflowAggregatesRequest
	wfAggRes, err := client.Dispatch(ctx, &protocol.GetWorkflowAggregatesRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: uuid.NewString()},
		Body: protocol.GetWorkflowAggregatesRequestBody{
			ApplicationName: []string{appName},
			GroupByStatus:   true,
			SelectCount:     true,
		},
	})
	if err != nil {
		t.Fatalf("GetWorkflowAggregates failed: %v", err)
	}
	wfAggResp, ok := wfAggRes.(*protocol.GetWorkflowAggregatesResponse)
	if !ok || len(wfAggResp.Output) == 0 {
		t.Fatalf("expected non-empty GetWorkflowAggregatesResponse, got %+v", wfAggRes)
	}
	if wfAggResp.Output[0].Group == nil || wfAggResp.Output[0].Group["status"] == nil || *wfAggResp.Output[0].Group["status"] != "SUCCESS" {
		t.Errorf("expected status SUCCESS in aggregates group, got %v", wfAggResp.Output[0].Group["status"])
	}
	if wfAggResp.Output[0].Count == nil || *wfAggResp.Output[0].Count < 1 {
		t.Errorf("expected count >= 1, got %v", wfAggResp.Output[0].Count)
	}

	// 2. GetStepAggregatesRequest
	stepAggRes, err := client.Dispatch(ctx, &protocol.GetStepAggregatesRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeGetStepAggregates, RequestID: uuid.NewString()},
		Body: protocol.GetStepAggregatesRequestBody{
			ApplicationName: []string{appName},
			GroupByStatus:   true,
			SelectCount:     true,
		},
	})
	if err != nil {
		t.Fatalf("GetStepAggregates failed: %v", err)
	}
	stepAggResp, ok := stepAggRes.(*protocol.GetStepAggregatesResponse)
	if !ok {
		t.Fatalf("expected GetStepAggregatesResponse, got %+v", stepAggRes)
	}
	_ = stepAggResp
}
