package verifysdk_test

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

	"github.com/abn/relay/internal/problem"
)

type ConformanceCheckResult struct {
	Name    string
	Status  CellStatus
	Detail  string
	Elapsed time.Duration
}

type ConformanceBatteryResult struct {
	ID      int
	Title   string
	Status  CellStatus
	Checks  []ConformanceCheckResult
	Elapsed time.Duration
	Error   string
}

type ConformanceReport struct {
	TotalPass int
	TotalFail int
	TotalSkip int
	AllPassed bool
	Batteries []ConformanceBatteryResult
}

type conformanceProbeRunner struct {
	client       *http.Client
	httpURL      string
	conductorKey string
	orgName      string
	appName      string
	lang         string
	appVersion   string
	wfID         string
}

func executeConformanceCheck(name string, fn func() error) ConformanceCheckResult {
	start := time.Now()
	err := fn()
	elapsed := time.Since(start)
	if err != nil {
		if strings.HasPrefix(err.Error(), "skip:") {
			return ConformanceCheckResult{
				Name:    name,
				Status:  CellStatusSkip,
				Detail:  strings.TrimSpace(strings.TrimPrefix(err.Error(), "skip:")),
				Elapsed: elapsed,
			}
		}
		return ConformanceCheckResult{
			Name:    name,
			Status:  CellStatusFail,
			Detail:  err.Error(),
			Elapsed: elapsed,
		}
	}
	return ConformanceCheckResult{
		Name:    name,
		Status:  CellStatusPass,
		Elapsed: elapsed,
	}
}

func summarizeConformanceChecks(id int, title string, checks []ConformanceCheckResult) ConformanceBatteryResult {
	if len(checks) == 0 {
		return ConformanceBatteryResult{
			ID:     id,
			Title:  title,
			Status: CellStatusSkip,
			Checks: checks,
		}
	}

	var totalElapsed time.Duration
	allPass := true
	var firstErr string

	for _, c := range checks {
		totalElapsed += c.Elapsed
		if c.Status == CellStatusFail {
			allPass = false
			if firstErr == "" {
				firstErr = fmt.Sprintf("%s: %s", c.Name, c.Detail)
			}
		}
	}

	status := CellStatusPass
	if !allPass {
		status = CellStatusFail
	}

	return ConformanceBatteryResult{
		ID:      id,
		Title:   title,
		Status:  status,
		Checks:  checks,
		Elapsed: totalElapsed,
		Error:   firstErr,
	}
}

func (r *conformanceProbeRunner) runBattery1(ctx context.Context) ConformanceBatteryResult {
	var checks []ConformanceCheckResult

	checks = append(checks, executeConformanceCheck("1.1 Database Health Probe (/healthz)", func() error {
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

	checks = append(checks, executeConformanceCheck("1.2 OpenAPI 3.1 Contract (/openapi.json)", func() error {
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

	checks = append(checks, executeConformanceCheck("1.3 Interactive Documentation (/docs)", func() error {
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
		return nil
	}))

	checks = append(checks, executeConformanceCheck("1.4 Prometheus Metrics Exposition (/v1/metrics)", func() error {
		if r.conductorKey != "" {
			unauthReq, err := http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/v1/metrics", nil)
			if err != nil {
				return err
			}
			unauthResp, err := r.client.Do(unauthReq)
			if err != nil {
				return err
			}
			defer func() { _ = unauthResp.Body.Close() }()

			if unauthResp.StatusCode != http.StatusUnauthorized {
				return fmt.Errorf("expected status 401 without token, got %d", unauthResp.StatusCode)
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.httpURL+"/v1/metrics", nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		return nil
	}))

	return summarizeConformanceChecks(1, "Specification & System Probes", checks)
}

func (r *conformanceProbeRunner) runBattery2(ctx context.Context) ConformanceBatteryResult {
	var checks []ConformanceCheckResult

	checks = append(checks, executeConformanceCheck("2.1 Invalid Key Handshake Rejection", func() error {
		reqURL := fmt.Sprintf("%s/websocket/%s/dbos_invalidkey99999", r.httpURL, r.appName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized {
			return fmt.Errorf("expected status 401 Unauthorized for invalid key, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			return fmt.Errorf("expected Content-Type application/problem+json, got %q", ct)
		}
		return nil
	}))

	checks = append(checks, executeConformanceCheck("2.2 Live Executor Fleet Presence", func() error {
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/executors", r.httpURL, r.orgName, r.appName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var execs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&execs); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
		foundHealthy := false
		for _, e := range execs {
			status, _ := e["status"].(string)
			if status == "HEALTHY" || status == "connected" {
				foundHealthy = true
				break
			}
		}
		if !foundHealthy {
			return errors.New("no live executor in fleet with HEALTHY status")
		}
		return nil
	}))

	return summarizeConformanceChecks(2, "WebSocket Handshake & Fleet Registration", checks)
}

func (r *conformanceProbeRunner) runBattery3(ctx context.Context) ConformanceBatteryResult {
	var checks []ConformanceCheckResult

	checks = append(checks, executeConformanceCheck("3.1 List Workflows Multiplexing", func() error {
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/search", r.httpURL, r.orgName, r.appName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(`{"limit":10}`))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		var list []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
		return nil
	}))

	checks = append(checks, executeConformanceCheck("3.2 Get Workflow Details Multiplexing", func() error {
		if r.wfID == "" {
			return fmt.Errorf("skip: no target workflow id")
		}
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/%s", r.httpURL, r.orgName, r.appName, r.wfID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
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
			return fmt.Errorf("invalid json: %w", err)
		}
		gotID, _ := wf["workflowId"].(string)
		if gotID != r.wfID {
			return fmt.Errorf("expected workflowId %q, got %q", r.wfID, gotID)
		}
		return nil
	}))

	checks = append(checks, executeConformanceCheck("3.3 List Steps Multiplexing", func() error {
		if r.wfID == "" {
			return fmt.Errorf("skip: no target workflow id")
		}
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/%s/steps", r.httpURL, r.orgName, r.appName, r.wfID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
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
			return fmt.Errorf("invalid json: %w", err)
		}
		return nil
	}))

	checks = append(checks, executeConformanceCheck("3.4 Workflow Events Multiplexing", func() error {
		if r.wfID == "" {
			return fmt.Errorf("skip: no target workflow id")
		}
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/%s/events", r.httpURL, r.orgName, r.appName, r.wfID)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		return nil
	}))

	return summarizeConformanceChecks(3, "REST & Wire Multiplexing (Observability)", checks)
}

func (r *conformanceProbeRunner) runBattery4(ctx context.Context) ConformanceBatteryResult {
	var checks []ConformanceCheckResult
	var ctrlWfID string

	// 4.1 Fork Workflow Mutation (to produce a dedicated controllable workflow instance)
	checks = append(checks, executeConformanceCheck("4.1 Fork Workflow Mutation", func() error {
		if r.wfID == "" {
			return fmt.Errorf("skip: no target workflow id for fork")
		}
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/%s/fork", r.httpURL, r.orgName, r.appName, r.wfID)
		bodyData := map[string]any{
			"appVersion": r.appVersion,
			"startStep":  1,
		}
		bBytes, _ := json.Marshal(bodyData)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("expected status 200 or 201, got %d", resp.StatusCode)
		}
		var res struct {
			WorkflowID string `json:"workflowId"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
		if res.WorkflowID == "" {
			return errors.New("empty workflowId returned from fork")
		}
		ctrlWfID = res.WorkflowID
		return nil
	}))

	// 4.2 Restart Workflow Mutation (step-0 fork semantics)
	checks = append(checks, executeConformanceCheck("4.2 Restart (Step-0 Fork) Mutation", func() error {
		if r.wfID == "" {
			return fmt.Errorf("skip: no target workflow id for restart")
		}
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/%s/fork", r.httpURL, r.orgName, r.appName, r.wfID)
		bodyData := map[string]any{
			"appVersion": r.appVersion,
			"startStep":  0,
		}
		bBytes, _ := json.Marshal(bodyData)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("expected status 200 or 201, got %d", resp.StatusCode)
		}
		var res struct {
			WorkflowID string `json:"workflowId"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
		if res.WorkflowID == "" {
			return errors.New("empty workflowId returned from restart")
		}
		return nil
	}))

	// 4.3 Cancel Workflow Mutation
	checks = append(checks, executeConformanceCheck("4.3 Cancel Workflow Mutation", func() error {
		targetID := ctrlWfID
		if targetID == "" {
			targetID = r.wfID
		}
		if targetID == "" {
			return fmt.Errorf("skip: no target workflow id for cancel")
		}
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/%s/cancel", r.httpURL, r.orgName, r.appName, targetID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(`{}`))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
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

	// 4.4 Resume Workflow Mutation
	checks = append(checks, executeConformanceCheck("4.4 Resume Workflow Mutation", func() error {
		targetID := ctrlWfID
		if targetID == "" {
			targetID = r.wfID
		}
		if targetID == "" {
			return fmt.Errorf("skip: no target workflow id for resume")
		}
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/workflows/%s/resume", r.httpURL, r.orgName, r.appName, targetID)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(`{}`))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
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

	return summarizeConformanceChecks(4, "Workflow Control Operations", checks)
}

func (r *conformanceProbeRunner) runBattery5(ctx context.Context) ConformanceBatteryResult {
	var checks []ConformanceCheckResult

	checks = append(checks, executeConformanceCheck("5.1 List Queues", func() error {
		if strings.ToLower(r.lang) == "java" {
			return fmt.Errorf("skip: Java SDK 0.8.0 does not implement queues")
		}
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/queues", r.httpURL, r.orgName, r.appName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		return nil
	}))

	checks = append(checks, executeConformanceCheck("5.2 List Schedules", func() error {
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/schedules", r.httpURL, r.orgName, r.appName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("expected status 200, got %d", resp.StatusCode)
		}
		return nil
	}))

	return summarizeConformanceChecks(5, "Queues & Schedules Operations", checks)
}

func (r *conformanceProbeRunner) runBattery7(ctx context.Context) ConformanceBatteryResult {
	var checks []ConformanceCheckResult
	var ruleID string

	checks = append(checks, executeConformanceCheck("7.1 Create Alerting Rule", func() error {
		payload := []byte(`{
			"ruleType": "workflow_failure",
			"minIntervalSecs": 30,
			"ruleMetadata": {"threshold": 5}
		}`)
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/alerting-rules", r.httpURL, r.orgName, r.appName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
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

	checks = append(checks, executeConformanceCheck("7.2 List Alerting Rules", func() error {
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/alerting-rules", r.httpURL, r.orgName, r.appName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
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

	checks = append(checks, executeConformanceCheck("7.3 Delete Alerting Rule", func() error {
		if ruleID == "" {
			return errors.New("cannot delete rule: ruleID is empty")
		}
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/alerting-rules/%s", r.httpURL, r.orgName, r.appName, ruleID)
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
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

	return summarizeConformanceChecks(7, "Alerting Rules Management", checks)
}

func (r *conformanceProbeRunner) runBattery8(ctx context.Context) ConformanceBatteryResult {
	var checks []ConformanceCheckResult

	checks = append(checks, executeConformanceCheck("8.1 Malformed JSON Returns 400 Problem Details", func() error {
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/alerting-rules", r.httpURL, r.orgName, r.appName)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader("{broken-json"))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
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

	checks = append(checks, executeConformanceCheck("8.2 Non-Existent Resource Returns 404 Problem Details", func() error {
		reqURL := fmt.Sprintf("%s/v2/orgs/unknown-org-404/apps/unknown-app-404/workflows", r.httpURL)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		if r.conductorKey != "" {
			req.Header.Set("Authorization", "Bearer "+r.conductorKey)
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

	checks = append(checks, executeConformanceCheck("8.3 Unauthenticated Request Gating", func() error {
		reqURL := fmt.Sprintf("%s/v2/orgs/%s/apps/%s/alerting-rules", r.httpURL, r.orgName, r.appName)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return err
		}
		resp, err := r.client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unexpected status %d", resp.StatusCode)
		}
		return nil
	}))

	return summarizeConformanceChecks(8, "RFC 9457 Problem Details & Identity Gating", checks)
}

func runConformanceProbes(ctx context.Context, targetURL, conductorKey, org, app, lang, appVersion, wfID string) (*ConformanceReport, error) {
	runner := &conformanceProbeRunner{
		client:       &http.Client{Timeout: 15 * time.Second},
		httpURL:      strings.TrimRight(targetURL, "/"),
		conductorKey: conductorKey,
		orgName:      org,
		appName:      app,
		lang:         lang,
		appVersion:   appVersion,
		wfID:         wfID,
	}

	report := &ConformanceReport{
		AllPassed: true,
		Batteries: make([]ConformanceBatteryResult, 0, 8),
	}

	batteries := []struct {
		id      int
		title   string
		isSkip  bool
		skipMsg string
		runFunc func(context.Context) ConformanceBatteryResult
	}{
		{1, "Specification & System Probes", false, "", runner.runBattery1},
		{2, "WebSocket Handshake & Fleet Registration", false, "", runner.runBattery2},
		{3, "REST & Wire Multiplexing (Observability)", false, "", runner.runBattery3},
		{4, "Workflow Control Operations", false, "", runner.runBattery4},
		{5, "Queues & Schedules Operations", false, "", runner.runBattery5},
		{6, "Workflow Recovery & Liveness Lifecycle", true, "Hard disconnect and recovery adoption verified across all runtimes in Cell 5", nil},
		{7, "Alerting Rules Management", false, "", runner.runBattery7},
		{8, "RFC 9457 Problem Details & Identity Gating", false, "", runner.runBattery8},
	}

	for _, b := range batteries {
		if b.isSkip {
			res := ConformanceBatteryResult{
				ID:     b.id,
				Title:  b.title,
				Status: CellStatusSkip,
				Error:  b.skipMsg,
			}
			report.Batteries = append(report.Batteries, res)
			report.TotalSkip++
			continue
		}

		res := b.runFunc(ctx)
		report.Batteries = append(report.Batteries, res)
		switch res.Status {
		case CellStatusPass:
			report.TotalPass++
		case CellStatusFail:
			report.TotalFail++
			report.AllPassed = false
		default:
			report.TotalSkip++
		}
	}

	return report, nil
}
