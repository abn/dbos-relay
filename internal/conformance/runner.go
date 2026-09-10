package conformance

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Runner orchestrates the execution of conformance test batteries.
type Runner struct {
	cfg     Config
	client  *http.Client
	httpURL string
	wsURL   string
}

// NewRunner creates a new conformance test runner with normalized URLs.
func NewRunner(cfg Config) *Runner {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.OrgName == "" {
		cfg.OrgName = "acme"
	}
	if cfg.AppName == "" {
		cfg.AppName = "conformance-app"
	}

	rawURL := strings.TrimRight(cfg.TargetURL, "/")
	httpURL := rawURL
	if !strings.HasPrefix(httpURL, "http://") && !strings.HasPrefix(httpURL, "https://") {
		httpURL = "http://" + httpURL
	}

	wsURL := httpURL
	if strings.HasPrefix(wsURL, "https://") {
		wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://")
	} else {
		wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://")
	}

	return &Runner{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		httpURL: httpURL,
		wsURL:   wsURL,
	}
}

// Run executes all 8 conformance batteries and returns an aggregated report.
func (r *Runner) Run(ctx context.Context) (*Report, error) {
	startTime := time.Now()
	report := &Report{
		TargetURL: r.httpURL,
		Timestamp: startTime,
		Batteries: make([]BatteryResult, 0, 8),
		AllPassed: true,
	}

	r.logf("Starting DBOS Conductor Conformance Test Suite against %s\n", r.httpURL)
	r.logf("Organization: %s | Application: %s\n\n", r.cfg.OrgName, r.cfg.AppName)

	batteries := []struct {
		id    int
		title string
		fn    func(context.Context) BatteryResult
	}{
		{1, "Specification & System Probes", r.runBattery1Spec},
		{2, "WebSocket Handshake & Fleet Registration", r.runBattery2Handshake},
		{3, "REST & Wire Multiplexing (Observability)", r.runBattery3Observability},
		{4, "Workflow Control Operations", r.runBattery4Control},
		{5, "Queues & Schedules Operations", r.runBattery5QueuesSchedules},
		{6, "Workflow Recovery & Liveness Lifecycle", r.runBattery6Recovery},
		{7, "Alerting Rules Management", r.runBattery7Alerting},
		{8, "RFC 9457 Problem Details & Identity Gating", r.runBattery8ProblemDetails},
	}

	for _, b := range batteries {
		shouldSkip := false
		for _, skipID := range r.cfg.SkipBatteryIDs {
			if b.id == skipID {
				shouldSkip = true
				break
			}
		}

		if shouldSkip {
			res := BatteryResult{
				ID:     b.id,
				Title:  b.title,
				Status: StatusSkip,
				Error:  r.cfg.SkipReason,
			}
			report.Batteries = append(report.Batteries, res)
			report.TotalSkip++
			r.logf("[SKIP] Battery %d: %s (%s)\n\n", b.id, b.title, r.cfg.SkipReason)
			continue
		}

		r.logf("[RUN] Battery %d: %s...\n", b.id, b.title)
		res := b.fn(ctx)
		report.Batteries = append(report.Batteries, res)

		switch res.Status {
		case StatusPass:
			report.TotalPass++
			r.logf("[PASS] Battery %d: %s (%s)\n\n", b.id, b.title, res.Elapsed.Round(time.Millisecond))
		case StatusFail:
			report.TotalFail++
			report.AllPassed = false
			r.logf("[FAIL] Battery %d: %s - %s (%s)\n\n", b.id, b.title, res.Error, res.Elapsed.Round(time.Millisecond))
		default:
			report.TotalSkip++
			r.logf("[SKIP] Battery %d: %s\n\n", b.id, b.title)
		}
	}

	report.Duration = time.Since(startTime)
	return report, nil
}

func (r *Runner) logf(format string, args ...any) {
	if r.cfg.Output != nil {
		_, _ = fmt.Fprintf(r.cfg.Output, format, args...)
	}
}

func executeCheck(name string, fn func() error) CheckResult {
	start := time.Now()
	err := fn()
	elapsed := time.Since(start)

	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  StatusFail,
			Detail:  err.Error(),
			Elapsed: elapsed,
		}
	}

	return CheckResult{
		Name:    name,
		Status:  StatusPass,
		Elapsed: elapsed,
	}
}

func summarizeChecks(id int, title string, checks []CheckResult) BatteryResult {
	var totalElapsed time.Duration
	allPass := true
	var firstErr string

	for _, c := range checks {
		totalElapsed += c.Elapsed
		if c.Status == StatusFail {
			allPass = false
			if firstErr == "" {
				firstErr = fmt.Sprintf("%s: %s", c.Name, c.Detail)
			}
		}
	}

	status := StatusPass
	if !allPass {
		status = StatusFail
	}

	return BatteryResult{
		ID:      id,
		Title:   title,
		Status:  status,
		Checks:  checks,
		Elapsed: totalElapsed,
		Error:   firstErr,
	}
}

// Run is a convenience function that initializes a Runner and executes all batteries.
func Run(ctx context.Context, cfg Config) (*Report, error) {
	runner := NewRunner(cfg)
	return runner.Run(ctx)
}
