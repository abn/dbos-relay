package dataplane_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/protocol"
)

// TestDirectSQLiteOperations exercises the real MIT Go client against an
// embedded SQLite system database. It proves read, control, schedule, queue,
// version, and recovery operations without the Conductor connector or a
// server. It does not simulate a process kill; exactly-once crash recovery is
// covered by the chaos suite and the recovery prototype.
func TestDirectSQLiteOperations(t *testing.T) {
	dbPath := t.TempDir() + "/sys.db"
	dbURL := "sqlite:" + dbPath
	appName := "direct-sqlite"
	appVersion := "v1"
	executorID := "exec-app"

	var stepRuns atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }

	blockingWorkflow := func(ctx dbos.Context, input string) (string, error) {
		_, err := dbos.RunAsStep(ctx, func(stepCtx context.Context) (string, error) {
			stepRuns.Add(1)
			startedOnce.Do(func() { close(started) })
			return "step-ok", nil
		})
		if err != nil {
			return "", err
		}
		<-release
		return "done", nil
	}

	appCtx, err := dbos.NewContext(context.Background(), dbos.Config{
		AppName:            appName,
		DatabaseURL:        dbURL,
		ApplicationVersion: appVersion,
		ExecutorID:         executorID,
	})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	immediateWorkflow := func(ctx dbos.Context, input string) (string, error) {
		return "ok", nil
	}
	dbos.RegisterWorkflow(appCtx, blockingWorkflow)
	dbos.RegisterWorkflow(appCtx, immediateWorkflow, dbos.WithWorkflowName("immediate"))
	if err := dbos.Launch(appCtx); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer func() {
		closeRelease()
		_ = dbos.Shutdown(appCtx, 5*time.Second)
	}()

	handle, err := dbos.RunWorkflow(appCtx, blockingWorkflow, "input")
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("workflow did not start")
	}

	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		ApplicationName:  appName,
		DatabaseURL:      dbURL,
		Mode:             dataplane.ModeReadWrite,
		Integration:      dataplane.IntegrationDirect,
		MaxConnections:   2,
		StatementTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSDKClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	listResp, err := client.Dispatch(context.Background(), &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "list"},
		Body:     protocol.ListWorkflowsRequestBody{WorkflowUUIDs: []string{handle.GetWorkflowID()}},
	})
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	list, ok := listResp.(*protocol.ListWorkflowsResponse)
	if !ok || len(list.Output) != 1 {
		t.Fatalf("unexpected list response: %#v", listResp)
	}
	if list.Output[0].WorkflowUUID != handle.GetWorkflowID() {
		t.Fatalf("unexpected workflow id: %s", list.Output[0].WorkflowUUID)
	}

	stepResp, err := client.Dispatch(context.Background(), &protocol.ListStepsRequest{
		Envelope:   protocol.Envelope{Type: protocol.MessageTypeListSteps, RequestID: "steps"},
		WorkflowID: handle.GetWorkflowID(),
	})
	if err != nil {
		t.Fatalf("ListSteps: %v", err)
	}
	steps, ok := stepResp.(*protocol.ListStepsResponse)
	if !ok || steps.Output == nil || len(*steps.Output) != 1 {
		t.Fatalf("unexpected steps response: %#v", stepResp)
	}

	ids, err := client.Recover(context.Background(), []string{executorID}, appVersion)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(ids) != 1 || ids[0] != handle.GetWorkflowID() {
		t.Fatalf("unexpected recovered ids: %v", ids)
	}

	if err := appCtx.CreateSchedule(appCtx, dbos.ScheduleSpec{
		ScheduleName: "yearly-report",
		Schedule:     "0 0 0 1 1 *",
		WorkflowName: "immediate",
	}); err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	schedResp, err := client.Dispatch(context.Background(), &protocol.ListSchedulesRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListSchedules, RequestID: "schedules"},
		Body:     protocol.ListSchedulesRequestBody{ApplicationName: protocol.StringOrList{appName}},
	})
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	schedules, ok := schedResp.(*protocol.ListSchedulesResponse)
	if !ok || len(schedules.Output) != 1 || schedules.Output[0].ScheduleName != "yearly-report" {
		t.Fatalf("unexpected schedules response: %#v", schedResp)
	}
	if _, err := client.Dispatch(context.Background(), &protocol.PauseScheduleRequest{
		Envelope:     protocol.Envelope{Type: protocol.MessageTypePauseSchedule, RequestID: "pause"},
		ScheduleName: "yearly-report",
	}); err != nil {
		t.Fatalf("PauseSchedule: %v", err)
	}
	if _, err := client.Dispatch(context.Background(), &protocol.ResumeScheduleRequest{
		Envelope:     protocol.Envelope{Type: protocol.MessageTypeResumeSchedule, RequestID: "resume"},
		ScheduleName: "yearly-report",
	}); err != nil {
		t.Fatalf("ResumeSchedule: %v", err)
	}

	if _, err := dbos.RegisterQueue(appCtx, "direct-test-queue"); err != nil {
		t.Fatalf("RegisterQueue: %v", err)
	}
	queueResp, err := client.Dispatch(context.Background(), &protocol.ListQueuesRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListQueues, RequestID: "queues"},
		Body:     protocol.ListQueuesRequestBody{ApplicationName: protocol.StringOrList{appName}},
	})
	if err != nil {
		t.Fatalf("ListQueues: %v", err)
	}
	queues, ok := queueResp.(*protocol.ListQueuesResponse)
	if !ok || len(queues.Output) == 0 {
		t.Fatalf("unexpected queues response: %#v", queueResp)
	}

	versionResp, err := client.Dispatch(context.Background(), &protocol.ListApplicationVersionsRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeListApplicationVersions, RequestID: "versions"},
	})
	if err != nil {
		t.Fatalf("ListApplicationVersions: %v", err)
	}
	versions, ok := versionResp.(*protocol.ListApplicationVersionsResponse)
	if !ok || len(versions.Output) == 0 || versions.Output[0].Name != appVersion {
		t.Fatalf("unexpected versions response: %#v", versionResp)
	}

	closeRelease()
	if _, err := handle.GetResult(); err != nil {
		t.Fatalf("workflow result after recovery: %v", err)
	}
	if got := stepRuns.Load(); got != 1 {
		t.Fatalf("expected the durable step to run once, got %d", got)
	}
}
