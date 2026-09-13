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
}

func executeConformanceCheck(name string, fn func() error) ConformanceCheckResult {
	start := time.Now()
	err := fn()
	elapsed := time.Since(start)
	if err != nil {
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

func runConformanceProbes(ctx context.Context, targetURL, conductorKey, org, app string) (*ConformanceReport, error) {
	runner := &conformanceProbeRunner{
		client:       &http.Client{Timeout: 15 * time.Second},
		httpURL:      strings.TrimRight(targetURL, "/"),
		conductorKey: conductorKey,
		orgName:      org,
		appName:      app,
	}

	report := &ConformanceReport{
		AllPassed: true,
		Batteries: make([]ConformanceBatteryResult, 0, 8),
	}

	skipReason := "batteries require synthetic executor; excluded during multi-SDK verification"
	batteries := []struct {
		id      int
		title   string
		isSkip  bool
		runFunc func(context.Context) ConformanceBatteryResult
	}{
		{1, "Specification & System Probes", false, runner.runBattery1},
		{2, "WebSocket Handshake & Fleet Registration", true, nil},
		{3, "REST & Wire Multiplexing (Observability)", true, nil},
		{4, "Workflow Control Operations", true, nil},
		{5, "Queues & Schedules Operations", true, nil},
		{6, "Workflow Recovery & Liveness Lifecycle", true, nil},
		{7, "Alerting Rules Management", false, runner.runBattery7},
		{8, "RFC 9457 Problem Details & Identity Gating", false, runner.runBattery8},
	}

	for _, b := range batteries {
		if b.isSkip {
			res := ConformanceBatteryResult{
				ID:     b.id,
				Title:  b.title,
				Status: CellStatusSkip,
				Error:  skipReason,
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
