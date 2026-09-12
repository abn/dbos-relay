package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/abn/relay/internal/fakeexecutor"
	"github.com/abn/relay/internal/problem"
	"github.com/abn/relay/internal/protocol"
)

func (r *Runner) runBattery1Spec(ctx context.Context) BatteryResult {
	var checks []CheckResult

	// Check 1.1: GET /healthz
	checks = append(checks, executeCheck("1.1 Database Health Probe (/healthz)", func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/healthz", nil)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		var h struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(body, &h); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
		if h.Status != "ok" {
			return fmt.Errorf("expected status 'ok', got %q", h.Status)
		}
		return nil
	}))

	// Check 1.2: GET /openapi.json
	checks = append(checks, executeCheck("1.2 OpenAPI 3.1 Contract (/openapi.json)", func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/openapi.json", nil)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var doc map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			return fmt.Errorf("invalid openapi json: %w", err)
		}
		if _, ok := doc["openapi"]; !ok {
			return errors.New("missing openapi version field")
		}
		return nil
	}))

	// Check 1.3: GET /openapi-3.0.json
	checks = append(checks, executeCheck("1.3 OpenAPI 3.0 Contract (/openapi-3.0.json)", func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/openapi-3.0.json", nil)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var doc map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			return fmt.Errorf("invalid openapi json: %w", err)
		}
		if _, ok := doc["openapi"]; !ok {
			return errors.New("missing openapi version field")
		}
		return nil
	}))

	// Check 1.4: GET /openapi.yaml
	checks = append(checks, executeCheck("1.4 OpenAPI YAML Contract (/openapi.yaml)", func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/openapi.yaml", nil)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body), "openapi:") {
			return errors.New("response does not contain yaml openapi key")
		}
		return nil
	}))

	// Check 1.5: GET /docs
	checks = append(checks, executeCheck("1.5 Interactive Documentation (/docs)", func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/docs", nil)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body), "<html") && !strings.Contains(string(body), "<!DOCTYPE") {
			return errors.New("expected html document")
		}
		return nil
	}))

	// Check 1.6: GET /v1/metrics
	checks = append(checks, executeCheck("1.6 Prometheus Metrics Scrape (/v1/metrics)", func() error {
		// Verify missing token yields 401
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/v1/metrics", nil)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			return fmt.Errorf("expected status 401 without token, got %d", resp.StatusCode)
		}

		// Verify with token yields 200
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/v1/metrics", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		resp2, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp2.Body.Close() }()

		if resp2.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp2.StatusCode)
		}
		body, err := io.ReadAll(resp2.Body)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body), "dbos_") && !strings.Contains(string(body), "# TYPE") {
			return errors.New("response does not contain expected prometheus metrics")
		}
		return nil
	}))

	return summarizeChecks(1, "Specification & System Probes", checks)
}

func (r *Runner) runBattery2Handshake(ctx context.Context) BatteryResult {
	var checks []CheckResult

	// Check 2.1: Invalid Key Rejected
	checks = append(checks, executeCheck("2.1 Invalid Key Handshake Rejection", func() error {
		badURL := fmt.Sprintf("%s/websocket/%s/dbos_invalidkey99999", r.wsURL, r.cfg.AppName)
		conn, resp, err := websocket.Dial(ctx, badURL, nil)
		if conn != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return errors.New("dial unexpectedly succeeded with invalid key")
		}
		if resp == nil {
			return fmt.Errorf("expected http response on rejected upgrade, got: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			return fmt.Errorf("expected status 401 Unauthorized, got %d", resp.StatusCode)
		}
		return nil
	}))

	// Check 2.2: Valid Handshake & Fleet Registration
	checks = append(checks, executeCheck("2.2 Valid Key Handshake & Fleet Registration", func() error {
		fe := fakeexecutor.New(fakeexecutor.Options{
			URL:                r.httpURL,
			AppName:            r.cfg.AppName,
			ConductorKey:       r.cfg.ConductorKey,
			ExecutorID:         "conformance-exec-1",
			ApplicationVersion: "v1.0.0",
			Language:           "python",
			Hostname:           "conformance-host",
		})

		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := fe.Connect(connectCtx); err != nil {
			return fmt.Errorf("fakeexecutor connect failed: %w", err)
		}
		go func() {
			_ = fe.Run(ctx)
		}()

		// Allow server to process handshake frame
		time.Sleep(150 * time.Millisecond)

		// Verify executor appears via REST API
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/executors", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			_ = fe.Close()
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}

		resp, err := r.client.Do(req)
		if err != nil {
			_ = fe.Close()
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			_ = fe.Close()
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var execs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&execs); err != nil {
			_ = fe.Close()
			return fmt.Errorf("invalid json response: %w", err)
		}

		found := false
		for _, ex := range execs {
			if id, ok := ex["executorId"].(string); ok && id == "conformance-exec-1" {
				if status, ok := ex["status"].(string); ok && status == "HEALTHY" {
					found = true
					break
				}
			}
		}

		_ = fe.Close()
		if !found {
			return errors.New("registered executor not found with HEALTHY status in fleet list")
		}
		return nil
	}))

	return summarizeChecks(2, "WebSocket Handshake & Fleet Registration", checks)
}

func (r *Runner) runBattery3Observability(ctx context.Context) BatteryResult {
	var checks []CheckResult

	wfName := "conformance_workflow"
	wfStatus := "SUCCESS"

	fe := fakeexecutor.New(fakeexecutor.Options{
		URL:                r.httpURL,
		AppName:            r.cfg.AppName,
		ConductorKey:       r.cfg.ConductorKey,
		ExecutorID:         "conformance-obs-exec",
		ApplicationVersion: "v1.0.0",
		Language:           "python",
		Hostname:           "obs-host",
	})

	fe.SetHandler(protocol.MessageTypeListWorkflows, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.ListWorkflowsRequest)
		return &protocol.ListWorkflowsResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListWorkflows,
				RequestID: req.RequestID,
			},
			Output: []protocol.ListWorkflowsResponseBody{
				{
					WorkflowUUID: "wf-conf-1",
					WorkflowName: &wfName,
					Status:       &wfStatus,
				},
			},
		}, nil
	})

	fe.SetHandler(protocol.MessageTypeGetWorkflow, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.GetWorkflowRequest)
		return &protocol.GetWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetWorkflow,
				RequestID: req.RequestID,
			},
			Output: &protocol.ListWorkflowsResponseBody{
				WorkflowUUID: req.WorkflowID,
				WorkflowName: &wfName,
				Status:       &wfStatus,
			},
		}, nil
	})

	fe.SetHandler(protocol.MessageTypeListSteps, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.ListStepsRequest)
		steps := []protocol.WorkflowStepsResponseBody{
			{FunctionID: 1, FunctionName: "step_one"},
			{FunctionID: 2, FunctionName: "step_two"},
		}
		return &protocol.ListStepsResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListSteps,
				RequestID: req.RequestID,
			},
			Output: &steps,
		}, nil
	})

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := fe.Connect(connectCtx); err != nil {
		return BatteryResult{
			ID:     3,
			Title:  "REST & Wire Multiplexing (Observability)",
			Status: StatusFail,
			Error:  fmt.Sprintf("failed to connect fake executor: %v", err),
		}
	}
	go func() { _ = fe.Run(ctx) }()
	defer func() { _ = fe.Close() }()
	time.Sleep(150 * time.Millisecond)

	// Check 3.1: List Workflows
	checks = append(checks, executeCheck("3.1 List Workflows Multiplexing", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var wfs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&wfs); err != nil {
			return fmt.Errorf("invalid json response: %w", err)
		}
		if len(wfs) == 0 {
			return errors.New("expected at least 1 workflow returned")
		}
		return nil
	}))

	// Check 3.2: Get Workflow
	checks = append(checks, executeCheck("3.2 Get Workflow Details Multiplexing", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/wf-conf-1", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var wf map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&wf); err != nil {
			return fmt.Errorf("invalid json response: %w", err)
		}
		if wf["workflowId"] != "wf-conf-1" {
			return fmt.Errorf("expected workflowId 'wf-conf-1', got %v", wf["workflowId"])
		}
		return nil
	}))

	// Check 3.3: List Steps
	checks = append(checks, executeCheck("3.3 List Steps Multiplexing", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/wf-conf-1/steps", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var steps []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&steps); err != nil {
			return fmt.Errorf("invalid json response: %w", err)
		}
		if len(steps) != 2 {
			return fmt.Errorf("expected 2 steps, got %d", len(steps))
		}
		return nil
	}))

	return summarizeChecks(3, "REST & Wire Multiplexing (Observability)", checks)
}

func (r *Runner) runBattery4Control(ctx context.Context) BatteryResult {
	var checks []CheckResult

	fe := fakeexecutor.New(fakeexecutor.Options{
		URL:                r.httpURL,
		AppName:            r.cfg.AppName,
		ConductorKey:       r.cfg.ConductorKey,
		ExecutorID:         "conformance-ctrl-exec",
		ApplicationVersion: "v1.0.0",
		Language:           "python",
		Hostname:           "ctrl-host",
	})

	fe.SetHandler(protocol.MessageTypeCancel, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.CancelWorkflowRequest)
		return &protocol.CancelWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeCancel,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil
	})

	fe.SetHandler(protocol.MessageTypeResume, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.ResumeWorkflowRequest)
		return &protocol.ResumeWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeResume,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil
	})

	fe.SetHandler(protocol.MessageTypeForkWorkflow, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.ForkWorkflowRequest)
		newID := req.Body.WorkflowID + "-forked"
		return &protocol.ForkWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeForkWorkflow,
				RequestID: req.RequestID,
			},
			NewWorkflowID: &newID,
		}, nil
	})

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := fe.Connect(connectCtx); err != nil {
		return BatteryResult{
			ID:     4,
			Title:  "Workflow Control Operations",
			Status: StatusFail,
			Error:  fmt.Sprintf("failed to connect fake executor: %v", err),
		}
	}
	go func() { _ = fe.Run(ctx) }()
	defer func() { _ = fe.Close() }()
	time.Sleep(150 * time.Millisecond)

	// Check 4.1: Cancel Workflow
	checks = append(checks, executeCheck("4.1 Cancel Workflow Mutation", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/wf-conf-1/cancel", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("expected status 200 or 204, got %d", resp.StatusCode)
		}
		return nil
	}))

	// Check 4.2: Resume Workflow
	checks = append(checks, executeCheck("4.2 Resume Workflow Mutation", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/wf-conf-1/resume", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("expected status 200 or 204, got %d", resp.StatusCode)
		}
		return nil
	}))

	// Check 4.3: Restart (Fork) Workflow
	checks = append(checks, executeCheck("4.3 Restart (Fork) Workflow Mutation", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/wf-conf-1/fork", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(`{"start_step": 0}`))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("expected status 200 or 201, got %d", resp.StatusCode)
		}
		return nil
	}))

	return summarizeChecks(4, "Workflow Control Operations", checks)
}

func (r *Runner) runBattery5QueuesSchedules(ctx context.Context) BatteryResult {
	var checks []CheckResult

	fe := fakeexecutor.New(fakeexecutor.Options{
		URL:                r.httpURL,
		AppName:            r.cfg.AppName,
		ConductorKey:       r.cfg.ConductorKey,
		ExecutorID:         "conformance-qs-exec",
		ApplicationVersion: "v1.0.0",
		Language:           "python",
		Hostname:           "qs-host",
	})

	fe.SetHandler(protocol.MessageTypeListQueues, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.ListQueuesRequest)
		return &protocol.ListQueuesResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListQueues,
				RequestID: req.RequestID,
			},
			Output: []protocol.QueueOutput{
				{Name: "default_queue"},
				{Name: "high_priority"},
			},
		}, nil
	})

	fe.SetHandler(protocol.MessageTypeListSchedules, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.ListSchedulesRequest)
		return &protocol.ListSchedulesResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListSchedules,
				RequestID: req.RequestID,
			},
			Output: []protocol.ScheduleOutput{
				{
					ScheduleID:   "s1",
					ScheduleName: "hourly_sync",
					WorkflowName: "sync_flow",
					Status:       "ACTIVE",
					Schedule:     "* * * * *",
				},
			},
		}, nil
	})

	fe.SetHandler(protocol.MessageTypePauseSchedule, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.PauseScheduleRequest)
		return &protocol.PauseScheduleResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypePauseSchedule,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil
	})

	fe.SetHandler(protocol.MessageTypeResumeSchedule, func(msg protocol.Message) (protocol.Message, error) {
		req, _ := msg.(*protocol.ResumeScheduleRequest)
		return &protocol.ResumeScheduleResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeResumeSchedule,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil
	})

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := fe.Connect(connectCtx); err != nil {
		return BatteryResult{
			ID:     5,
			Title:  "Queues & Schedules Operations",
			Status: StatusFail,
			Error:  fmt.Sprintf("failed to connect fake executor: %v", err),
		}
	}
	go func() { _ = fe.Run(ctx) }()
	defer func() { _ = fe.Close() }()
	time.Sleep(150 * time.Millisecond)

	// Check 5.1: List Queues
	checks = append(checks, executeCheck("5.1 List Queues", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/queues", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var q []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
		if len(q) == 0 {
			return errors.New("expected at least 1 queue returned")
		}
		return nil
	}))

	// Check 5.2: List Schedules
	checks = append(checks, executeCheck("5.2 List Schedules", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/schedules", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var s []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
		if len(s) == 0 {
			return errors.New("expected at least 1 schedule returned")
		}
		return nil
	}))

	// Check 5.3: Pause Schedule
	checks = append(checks, executeCheck("5.3 Pause Schedule", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/schedules/hourly_sync/pause", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("expected status 200 or 204, got %d", resp.StatusCode)
		}
		return nil
	}))

	// Check 5.4: Resume Schedule
	checks = append(checks, executeCheck("5.4 Resume Schedule", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/schedules/hourly_sync/resume", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("expected status 200 or 204, got %d", resp.StatusCode)
		}
		return nil
	}))

	return summarizeChecks(5, "Queues & Schedules Operations", checks)
}

func (r *Runner) runBattery6Recovery(ctx context.Context) BatteryResult {
	var checks []CheckResult

	// Check 6.1: Missed Heartbeat / Disconnect Detection
	checks = append(checks, executeCheck("6.1 Disconnect Detection & Status Transition", func() error {
		fe := fakeexecutor.New(fakeexecutor.Options{
			URL:                r.httpURL,
			AppName:            r.cfg.AppName,
			ConductorKey:       r.cfg.ConductorKey,
			ExecutorID:         "conformance-rec-dead",
			ApplicationVersion: "v1.0.0",
			Language:           "python",
			Hostname:           "crash-host",
		})

		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := fe.Connect(connectCtx); err != nil {
			return fmt.Errorf("connect failed: %w", err)
		}
		go func() { _ = fe.Run(ctx) }()
		time.Sleep(100 * time.Millisecond)

		// Hard disconnect
		_ = fe.Close()
		time.Sleep(200 * time.Millisecond)

		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/executors", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		var execs []map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&execs)

		for _, ex := range execs {
			if id, ok := ex["executorId"].(string); ok && id == "conformance-rec-dead" {
				status, _ := ex["status"].(string)
				if status == "HEALTHY" {
					return errors.New("closed executor still reported as HEALTHY")
				}
			}
		}
		return nil
	}))

	// Check 6.2: Recovery Adoption Readiness
	checks = append(checks, executeCheck("6.2 Replacement Executor Adoption Readiness", func() error {
		fe := fakeexecutor.New(fakeexecutor.Options{
			URL:                r.httpURL,
			AppName:            r.cfg.AppName,
			ConductorKey:       r.cfg.ConductorKey,
			ExecutorID:         "conformance-rec-alive",
			ApplicationVersion: "v1.0.0",
			Language:           "python",
			Hostname:           "alive-host",
		})

		fe.SetHandler(protocol.MessageTypeRecovery, func(msg protocol.Message) (protocol.Message, error) {
			req, _ := msg.(*protocol.RecoveryRequest)
			return &protocol.RecoveryResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeRecovery,
					RequestID: req.RequestID,
				},
				Success: true,
			}, nil
		})

		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := fe.Connect(connectCtx); err != nil {
			return fmt.Errorf("replacement connect failed: %w", err)
		}
		go func() { _ = fe.Run(ctx) }()
		defer func() { _ = fe.Close() }()
		time.Sleep(150 * time.Millisecond)

		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/executors", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		var execs []map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&execs)

		found := false
		for _, ex := range execs {
			if id, ok := ex["executorId"].(string); ok && id == "conformance-rec-alive" {
				if status, ok := ex["status"].(string); ok && status == "HEALTHY" {
					found = true
					break
				}
			}
		}
		if !found {
			return errors.New("replacement executor not healthy")
		}
		return nil
	}))

	return summarizeChecks(6, "Workflow Recovery & Liveness Lifecycle", checks)
}

func (r *Runner) runBattery7Alerting(ctx context.Context) BatteryResult {
	var checks []CheckResult
	var ruleID string

	// Check 7.1: Create Alerting Rule
	checks = append(checks, executeCheck("7.1 Create Alerting Rule", func() error {
		payload := []byte(`{
			"ruleType": "workflow_failure",
			"minIntervalSecs": 30,
			"ruleMetadata": {"threshold": 5}
		}`)
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/alerting-rules", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("expected status 200 or 201, got %d", resp.StatusCode)
		}
		var created map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return fmt.Errorf("invalid json response: %w", err)
		}
		if id, ok := created["id"].(string); ok && id != "" {
			ruleID = id
		} else if id, ok := created["ruleId"].(string); ok && id != "" {
			ruleID = id
		}
		return nil
	}))

	// Check 7.2: List Alerting Rules
	checks = append(checks, executeCheck("7.2 List Alerting Rules", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/alerting-rules", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var rules []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
		if len(rules) == 0 {
			return errors.New("expected at least 1 alerting rule")
		}
		return nil
	}))

	// Check 7.3: Delete Alerting Rule
	checks = append(checks, executeCheck("7.3 Delete Alerting Rule", func() error {
		if ruleID == "" {
			return nil // Skip if create didn't return an ID
		}
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/alerting-rules/%s", r.httpURL, r.cfg.OrgName, r.cfg.AppName, ruleID)
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("expected status 200 or 204, got %d", resp.StatusCode)
		}
		return nil
	}))

	return summarizeChecks(7, "Alerting Rules Management", checks)
}

func (r *Runner) runBattery8ProblemDetails(ctx context.Context) BatteryResult {
	var checks []CheckResult

	// Check 8.1: Malformed JSON -> 400 Problem Details
	checks = append(checks, executeCheck("8.1 Malformed JSON Returns 400 Problem Details", func() error {
		url := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/alerting-rules", r.httpURL, r.cfg.OrgName, r.cfg.AppName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{broken-json"))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("expected status 400, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			return fmt.Errorf("expected application/problem+json, got %q", ct)
		}
		var prob problem.Problem
		if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
			return fmt.Errorf("failed to decode RFC 9457 problem: %w", err)
		}
		if prob.Status != http.StatusBadRequest {
			return fmt.Errorf("expected problem status 400, got %d", prob.Status)
		}
		return nil
	}))

	// Check 8.2: Non-existent Resource -> 404 Problem Details
	checks = append(checks, executeCheck("8.2 Non-Existent Resource Returns 404 Problem Details", func() error {
		url := fmt.Sprintf("%s/v2/orgs/unknown-org-404/apps/unknown-app-404/workflows", r.httpURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if r.cfg.ConductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.cfg.ConductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusNotFound {
			return fmt.Errorf("expected status 404, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			return fmt.Errorf("expected application/problem+json, got %q", ct)
		}
		var prob problem.Problem
		if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
			return fmt.Errorf("failed to decode RFC 9457 problem: %w", err)
		}
		if prob.Status != http.StatusNotFound {
			return fmt.Errorf("expected problem status 404, got %d", prob.Status)
		}
		return nil
	}))

	// Check 8.3: No-Auth OAuth Stubs -> 404 Problem Details
	checks = append(checks, executeCheck("8.3 No-Auth OAuth Stubs Return 404 Problem Details", func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/v2/users/me", nil)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		// In no-auth mode, returns 404 Problem Details
		if resp.StatusCode != http.StatusNotFound {
			return fmt.Errorf("expected status 404 in no-auth mode, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			return fmt.Errorf("expected application/problem+json, got %q", ct)
		}
		return nil
	}))

	return summarizeChecks(8, "RFC 9457 Problem Details & Identity Gating", checks)
}
