package dataplane_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/protocol"
)

// TestDirectSQLiteMetrics reads workflow and step counts through the real MIT
// Go client and an embedded SQLite system database. The assertion follows the
// public SDK implementations: workflow_status grouped by name over created_at,
// and operation_outputs grouped by function_name over completed_at_epoch_ms.
func TestDirectSQLiteMetrics(t *testing.T) {
	dbURL := "sqlite:" + t.TempDir() + "/sys.db"
	appName := "direct-metrics"

	appCtx, err := dbos.NewContext(context.Background(), dbos.Config{
		AppName:            appName,
		DatabaseURL:        dbURL,
		ApplicationVersion: "v1",
		ExecutorID:         "exec-metrics",
	})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	workflow := func(ctx dbos.Context, input int) (int, error) {
		return dbos.RunAsStep(ctx, func(context.Context) (int, error) {
			return input * 2, nil
		})
	}
	dbos.RegisterWorkflow(appCtx, workflow, dbos.WithWorkflowName("metrics-workflow"))
	if err := dbos.Launch(appCtx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() { _ = dbos.Shutdown(appCtx, 5*time.Second) }()

	start := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	for i := 0; i < 2; i++ {
		handle, err := dbos.RunWorkflow(appCtx, workflow, i)
		if err != nil {
			t.Fatalf("RunWorkflow: %v", err)
		}
		if _, err := handle.GetResult(); err != nil {
			t.Fatalf("GetResult: %v", err)
		}
	}
	end := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)

	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		ApplicationName:  appName,
		DatabaseURL:      dbURL,
		Mode:             dataplane.ModeRead,
		Integration:      dataplane.IntegrationDirect,
		StatementTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	respMsg, err := client.Dispatch(context.Background(), &protocol.GetMetricsRequest{
		Envelope:        protocol.Envelope{Type: protocol.MessageTypeGetMetrics, RequestID: "metrics"},
		StartTime:       start,
		EndTime:         end,
		MetricClass:     "workflow_step_count",
		ApplicationName: []string{appName},
	})
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	resp, ok := respMsg.(*protocol.GetMetricsResponse)
	if !ok {
		t.Fatalf("unexpected metrics response: %#v", respMsg)
	}

	var workflowCount, stepCount *protocol.MetricData
	for i := range resp.Metrics {
		metric := &resp.Metrics[i]
		switch metric.MetricType {
		case "workflow_count":
			if metric.MetricName == "metrics-workflow" {
				workflowCount = metric
			}
		case "step_count":
			stepCount = metric
		}
	}
	if workflowCount == nil || workflowCount.Value != 2 {
		t.Fatalf("expected two metrics-workflow rows, got %#v", resp.Metrics)
	}
	if stepCount == nil || stepCount.Value != 2 {
		t.Fatalf("expected two completed steps, got %#v", resp.Metrics)
	}
}

// TestDirectSQLiteRetentionTimeAndActive applies a future time cutoff through
// the real client. Terminal workflows are deleted in batches while a PENDING
// workflow survives, matching the documented retention rule.
func TestDirectSQLiteRetentionTimeAndActive(t *testing.T) {
	dbURL := "sqlite:" + t.TempDir() + "/sys.db"
	appName := "direct-retention-time"

	var releaseOnce sync.Once
	release := make(chan struct{})
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	started := make(chan struct{})
	var startedOnce sync.Once

	appCtx, err := dbos.NewContext(context.Background(), dbos.Config{
		AppName:            appName,
		DatabaseURL:        dbURL,
		ApplicationVersion: "v1",
		ExecutorID:         "exec-retention-time",
	})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	completed := func(dbos.Context, string) (string, error) { return "done", nil }
	blocking := func(dbos.Context, string) (string, error) {
		startedOnce.Do(func() { close(started) })
		<-release
		return "released", nil
	}
	dbos.RegisterWorkflow(appCtx, completed, dbos.WithWorkflowName("completed"))
	dbos.RegisterWorkflow(appCtx, blocking, dbos.WithWorkflowName("blocking"))
	if err := dbos.Launch(appCtx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() {
		closeRelease()
		_ = dbos.Shutdown(appCtx, 5*time.Second)
	}()

	var completedIDs []string
	for i := 0; i < 3; i++ {
		handle, err := dbos.RunWorkflow(appCtx, completed, "x")
		if err != nil {
			t.Fatalf("RunWorkflow: %v", err)
		}
		if _, err := handle.GetResult(); err != nil {
			t.Fatalf("GetResult: %v", err)
		}
		completedIDs = append(completedIDs, handle.GetWorkflowID())
	}
	pendingHandle, err := dbos.RunWorkflow(appCtx, blocking, "x")
	if err != nil {
		t.Fatalf("RunWorkflow blocking: %v", err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("blocking workflow did not start")
	}

	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		ApplicationName:  appName,
		DatabaseURL:      dbURL,
		Mode:             dataplane.ModeReadWrite,
		Integration:      dataplane.IntegrationDirect,
		StatementTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	cutoff := int(time.Now().Add(time.Hour).UnixMilli())
	batch := 2
	respMsg, err := client.Dispatch(context.Background(), &protocol.RetentionRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeRetention, RequestID: "retention"},
		Body: protocol.RetentionRequestBody{
			GCCutoffEpochMs: &cutoff,
			GCBatchSize:     &batch,
		},
	})
	if err != nil {
		t.Fatalf("Retention: %v", err)
	}
	if resp, ok := respMsg.(*protocol.RetentionResponse); !ok || !resp.Success {
		t.Fatalf("unexpected retention response: %#v", respMsg)
	}

	remaining := listWorkflowIDs(t, client, append(completedIDs, pendingHandle.GetWorkflowID()))
	if len(remaining) != 1 || remaining[0] != pendingHandle.GetWorkflowID() {
		t.Fatalf("expected only the pending workflow to remain, got %v", remaining)
	}
}

// TestDirectSQLiteRetentionRowsThreshold keeps the newest terminal workflow
// when a rows threshold is set, even when a batch size of one forces several
// delete rounds.
func TestDirectSQLiteRetentionRowsThreshold(t *testing.T) {
	dbURL := "sqlite:" + t.TempDir() + "/sys.db"
	appName := "direct-retention-rows"

	appCtx, err := dbos.NewContext(context.Background(), dbos.Config{
		AppName:            appName,
		DatabaseURL:        dbURL,
		ApplicationVersion: "v1",
		ExecutorID:         "exec-retention-rows",
	})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	completed := func(dbos.Context, string) (string, error) { return "done", nil }
	dbos.RegisterWorkflow(appCtx, completed, dbos.WithWorkflowName("completed"))
	if err := dbos.Launch(appCtx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() { _ = dbos.Shutdown(appCtx, 5*time.Second) }()

	var ids []string
	for i := 0; i < 3; i++ {
		handle, err := dbos.RunWorkflow(appCtx, completed, "x")
		if err != nil {
			t.Fatalf("RunWorkflow: %v", err)
		}
		if _, err := handle.GetResult(); err != nil {
			t.Fatalf("GetResult: %v", err)
		}
		ids = append(ids, handle.GetWorkflowID())
		time.Sleep(10 * time.Millisecond)
	}

	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		ApplicationName:  appName,
		DatabaseURL:      dbURL,
		Mode:             dataplane.ModeReadWrite,
		Integration:      dataplane.IntegrationDirect,
		StatementTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	threshold := 1
	batch := 1
	if _, err := client.Dispatch(context.Background(), &protocol.RetentionRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeRetention, RequestID: "retention"},
		Body: protocol.RetentionRequestBody{
			GCRowsThreshold: &threshold,
			GCBatchSize:     &batch,
		},
	}); err != nil {
		t.Fatalf("Retention: %v", err)
	}

	newest := ids[len(ids)-1]
	remaining := listWorkflowIDs(t, client, ids)
	if len(remaining) != 1 || remaining[0] != newest {
		t.Fatalf("expected only the newest workflow %s to remain, got %v", newest, remaining)
	}
}

// TestDirectSQLiteRetentionGlobalTimeout cancels a PENDING workflow created
// before the timeout cutoff while leaving newer work alone.
func TestDirectSQLiteRetentionGlobalTimeout(t *testing.T) {
	dbURL := "sqlite:" + t.TempDir() + "/sys.db"
	appName := "direct-retention-timeout"

	var releaseOnce sync.Once
	release := make(chan struct{})
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	started := make(chan struct{})
	var startedOnce sync.Once

	appCtx, err := dbos.NewContext(context.Background(), dbos.Config{
		AppName:            appName,
		DatabaseURL:        dbURL,
		ApplicationVersion: "v1",
		ExecutorID:         "exec-retention-timeout",
	})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	blocking := func(dbos.Context, string) (string, error) {
		startedOnce.Do(func() { close(started) })
		<-release
		return "released", nil
	}
	dbos.RegisterWorkflow(appCtx, blocking, dbos.WithWorkflowName("blocking"))
	if err := dbos.Launch(appCtx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() {
		closeRelease()
		_ = dbos.Shutdown(appCtx, 5*time.Second)
	}()

	handle, err := dbos.RunWorkflow(appCtx, blocking, "x")
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("blocking workflow did not start")
	}

	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		ApplicationName:  appName,
		DatabaseURL:      dbURL,
		Mode:             dataplane.ModeReadWrite,
		Integration:      dataplane.IntegrationDirect,
		StatementTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	cutoff := int(time.Now().Add(time.Hour).UnixMilli())
	if _, err := client.Dispatch(context.Background(), &protocol.RetentionRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeRetention, RequestID: "retention"},
		Body: protocol.RetentionRequestBody{
			TimeoutCutoffEpochMs: &cutoff,
		},
	}); err != nil {
		t.Fatalf("Retention: %v", err)
	}

	respMsg, err := client.Dispatch(context.Background(), &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "list"},
		Body:     protocol.ListWorkflowsRequestBody{WorkflowUUIDs: []string{handle.GetWorkflowID()}},
	})
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	list, ok := respMsg.(*protocol.ListWorkflowsResponse)
	if !ok || len(list.Output) != 1 {
		t.Fatalf("unexpected list response: %#v", respMsg)
	}
	if list.Output[0].Status == nil || *list.Output[0].Status != string(dbos.WorkflowStatusCancelled) {
		t.Fatalf("expected CANCELLED, got %#v", list.Output[0].Status)
	}
}

func listWorkflowIDs(t *testing.T, client dataplane.Client, ids []string) []string {
	t.Helper()
	respMsg, err := client.Dispatch(context.Background(), &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "list"},
		Body:     protocol.ListWorkflowsRequestBody{WorkflowUUIDs: ids},
	})
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	resp, ok := respMsg.(*protocol.ListWorkflowsResponse)
	if !ok {
		t.Fatalf("unexpected list response: %#v", respMsg)
	}
	out := make([]string, len(resp.Output))
	for i, row := range resp.Output {
		out[i] = row.WorkflowUUID
	}
	return out
}

// TestDirectSQLiteGetEvent reads one workflow event by key through the real MIT
// Go client. The event is written by the workflow itself, so the test does not
// need a set-event extension or direct system table access.
func TestDirectSQLiteGetEvent(t *testing.T) {
	dbURL := "sqlite:" + t.TempDir() + "/sys.db"
	appName := "direct-event"

	appCtx, err := dbos.NewContext(context.Background(), dbos.Config{
		AppName:            appName,
		DatabaseURL:        dbURL,
		ApplicationVersion: "v1",
		ExecutorID:         "exec-event",
	})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	workflow := func(ctx dbos.Context, input int) (int, error) {
		if err := dbos.SetEvent(ctx, "status", map[string]any{"ready": true}); err != nil {
			return 0, err
		}
		return input, nil
	}
	dbos.RegisterWorkflow(appCtx, workflow, dbos.WithWorkflowName("event-workflow"))
	if err := dbos.Launch(appCtx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() { _ = dbos.Shutdown(appCtx, 5*time.Second) }()

	handle, err := dbos.RunWorkflow(appCtx, workflow, 7)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if _, err := handle.GetResult(); err != nil {
		t.Fatalf("GetResult: %v", err)
	}

	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		ApplicationName:  appName,
		DatabaseURL:      dbURL,
		Mode:             dataplane.ModeRead,
		Integration:      dataplane.IntegrationDirect,
		StatementTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	event, err := client.GetEvent(context.Background(), handle.GetWorkflowID(), "status")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	value, ok := event.Value.(map[string]any)
	if event.Key != "status" || !ok || value["ready"] != true {
		t.Fatalf("unexpected event: %+v", event)
	}

	if _, err := client.GetEvent(context.Background(), handle.GetWorkflowID(), "missing"); !errors.Is(err, dataplane.ErrEventNotFound) {
		t.Fatalf("expected ErrEventNotFound, got %v", err)
	}
}
