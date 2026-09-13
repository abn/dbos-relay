package conformance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abn/relay/internal/conformance"
)

func TestReportMarkdownFormatting(t *testing.T) {
	report := &conformance.Report{
		TargetURL: "http://localhost:8090",
		Timestamp: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
		Duration:  1250 * time.Millisecond,
		TotalPass: 1,
		TotalFail: 0,
		AllPassed: true,
		Batteries: []conformance.BatteryResult{
			{
				ID:      1,
				Title:   "Specification & System Probes",
				Status:  conformance.StatusPass,
				Elapsed: 250 * time.Millisecond,
				Checks: []conformance.CheckResult{
					{
						Name:    "1.1 Database Health Probe",
						Status:  conformance.StatusPass,
						Elapsed: 50 * time.Millisecond,
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := report.FormatMarkdown(&buf); err != nil {
		t.Fatalf("FormatMarkdown failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "CONFORMANT (100% PASS)") {
		t.Errorf("expected 100%% PASS indicator, got:\n%s", out)
	}
	if !strings.Contains(out, "B1") {
		t.Errorf("expected battery B1 in scorecard, got:\n%s", out)
	}
	if !strings.Contains(out, "1.1 Database Health Probe") {
		t.Errorf("expected check name in details, got:\n%s", out)
	}
}

func TestReportMarkdownFailureState(t *testing.T) {
	report := &conformance.Report{
		TargetURL: "http://localhost:8090",
		Timestamp: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
		Duration:  500 * time.Millisecond,
		TotalPass: 0,
		TotalFail: 1,
		AllPassed: false,
		Batteries: []conformance.BatteryResult{
			{
				ID:      2,
				Title:   "WebSocket Handshake",
				Status:  conformance.StatusFail,
				Error:   "dial timeout",
				Elapsed: 500 * time.Millisecond,
				Checks: []conformance.CheckResult{
					{
						Name:    "2.1 Handshake",
						Status:  conformance.StatusFail,
						Detail:  "dial timeout",
						Elapsed: 500 * time.Millisecond,
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := report.FormatMarkdown(&buf); err != nil {
		t.Fatalf("FormatMarkdown failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "NON-CONFORMANT") {
		t.Errorf("expected NON-CONFORMANT indicator, got:\n%s", out)
	}
	if !strings.Contains(out, "dial timeout") {
		t.Errorf("expected failure detail in output, got:\n%s", out)
	}
}

func TestSummarizeChecks_ScoringTableDriven(t *testing.T) {
	tests := []struct {
		name       string
		id         int
		title      string
		checks     []conformance.CheckResult
		wantStatus conformance.BatteryStatus
		wantErr    string
	}{
		{
			name:       "empty check slice yields StatusSkip",
			id:         1,
			title:      "Empty Battery",
			checks:     []conformance.CheckResult{},
			wantStatus: conformance.StatusSkip,
			wantErr:    "",
		},
		{
			name:  "all passing checks yields StatusPass",
			id:    2,
			title: "Passing Battery",
			checks: []conformance.CheckResult{
				{Name: "2.1", Status: conformance.StatusPass, Elapsed: 10 * time.Millisecond},
				{Name: "2.2", Status: conformance.StatusPass, Elapsed: 20 * time.Millisecond},
			},
			wantStatus: conformance.StatusPass,
			wantErr:    "",
		},
		{
			name:  "mixed checks yields StatusFail with first error",
			id:    3,
			title: "Mixed Battery",
			checks: []conformance.CheckResult{
				{Name: "3.1", Status: conformance.StatusPass, Elapsed: 10 * time.Millisecond},
				{Name: "3.2", Status: conformance.StatusFail, Detail: "failed detail", Elapsed: 15 * time.Millisecond},
				{Name: "3.3", Status: conformance.StatusFail, Detail: "second failure", Elapsed: 5 * time.Millisecond},
			},
			wantStatus: conformance.StatusFail,
			wantErr:    "3.2: failed detail",
		},
		{
			name:  "all failing checks yields StatusFail",
			id:    4,
			title: "Failing Battery",
			checks: []conformance.CheckResult{
				{Name: "4.1", Status: conformance.StatusFail, Detail: "err1", Elapsed: 5 * time.Millisecond},
			},
			wantStatus: conformance.StatusFail,
			wantErr:    "4.1: err1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := conformance.SummarizeChecks(tc.id, tc.title, tc.checks)
			if res.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", res.Status, tc.wantStatus)
			}
			if res.Error != tc.wantErr {
				t.Errorf("error = %q, want %q", res.Error, tc.wantErr)
			}
			if len(res.Checks) != len(tc.checks) {
				t.Errorf("checks len = %d, want %d", len(res.Checks), len(tc.checks))
			}
		})
	}
}

func TestRunner_TimeoutAndClientConfiguration(t *testing.T) {
	cfg := conformance.Config{
		TargetURL: "http://127.0.0.1:9999",
		Timeout:   45 * time.Second,
	}
	runner := conformance.NewRunner(cfg)
	if runner == nil {
		t.Fatal("expected non-nil runner")
	}

	// Default fallback when 0
	defRunner := conformance.NewRunner(conformance.Config{TargetURL: "http://127.0.0.1:9999"})
	if defRunner == nil {
		t.Fatal("expected non-nil default runner")
	}
}

func TestRunner_RunAllSkippedNotAllPassed(t *testing.T) {
	cfg := conformance.Config{
		TargetURL:      "http://127.0.0.1:9999",
		SkipBatteryIDs: []int{1, 2, 3, 4, 5, 6, 7, 8},
		SkipReason:     "testing all skip",
	}
	runner := conformance.NewRunner(cfg)
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.AllPassed {
		t.Errorf("expected AllPassed to be false when all batteries skipped")
	}
	if report.TotalPass != 0 {
		t.Errorf("expected TotalPass = 0, got %d", report.TotalPass)
	}
	if report.TotalSkip != 8 {
		t.Errorf("expected TotalSkip = 8, got %d", report.TotalSkip)
	}
}

func TestRunner_RunCheckFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := conformance.Config{
		TargetURL:      server.URL,
		SkipBatteryIDs: []int{2, 3, 4, 5, 6, 7, 8}, // only run battery 1
		SkipReason:     "testing battery 1 failure",
	}
	runner := conformance.NewRunner(cfg)
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.AllPassed {
		t.Errorf("expected AllPassed = false when checks fail")
	}
	if report.TotalFail != 1 {
		t.Errorf("expected TotalFail = 1, got %d", report.TotalFail)
	}
}

func TestRunner_PollHelpers(t *testing.T) {
	t.Run("waitForExecutorHealthy succeeds when executor is healthy", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"executorId": "test-exec-1", "status": "HEALTHY"},
			})
		}))
		defer ts.Close()

		runner := conformance.NewRunner(conformance.Config{
			TargetURL: ts.URL,
			OrgName:   "testorg",
			AppName:   "testapp",
		})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := conformance.WaitForExecutorHealthy(runner, ctx, "test-exec-1")
		if err != nil {
			t.Fatalf("expected healthy executor to succeed, got: %v", err)
		}
	})

	t.Run("waitForExecutorHealthy times out when executor is missing", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}))
		defer ts.Close()

		runner := conformance.NewRunner(conformance.Config{
			TargetURL: ts.URL,
			OrgName:   "testorg",
			AppName:   "testapp",
		})
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := conformance.WaitForExecutorHealthy(runner, ctx, "test-exec-1")
		if err == nil {
			t.Fatal("expected timeout error when executor is missing, got nil")
		}
	})

	t.Run("waitForExecutorUnhealthy succeeds when executor is DISCONNECTED or DEAD", func(t *testing.T) {
		for _, status := range []string{"DISCONNECTED", "DEAD"} {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]map[string]any{
					{"executorId": "test-exec-dead", "status": status},
				})
			}))

			runner := conformance.NewRunner(conformance.Config{
				TargetURL: ts.URL,
				OrgName:   "testorg",
				AppName:   "testapp",
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

			err := conformance.WaitForExecutorUnhealthy(runner, ctx, "test-exec-dead")
			cancel()
			ts.Close()
			if err != nil {
				t.Fatalf("expected status %s to succeed, got: %v", status, err)
			}
		}
	})

	t.Run("waitForExecutorUnhealthy handles executor_id field name", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"executor_id": "test-exec-dead", "status": "DEAD"},
			})
		}))
		defer ts.Close()

		runner := conformance.NewRunner(conformance.Config{
			TargetURL: ts.URL,
			OrgName:   "testorg",
			AppName:   "testapp",
		})
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err := conformance.WaitForExecutorUnhealthy(runner, ctx, "test-exec-dead")
		if err != nil {
			t.Fatalf("expected executor_id format to succeed, got: %v", err)
		}
	})

	t.Run("waitForExecutorUnhealthy does not succeed immediately on empty list or healthy executor", func(t *testing.T) {
		tsEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}))
		defer tsEmpty.Close()

		runnerEmpty := conformance.NewRunner(conformance.Config{
			TargetURL: tsEmpty.URL,
			OrgName:   "testorg",
			AppName:   "testapp",
		})
		ctxEmpty, cancelEmpty := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancelEmpty()

		err := conformance.WaitForExecutorUnhealthy(runnerEmpty, ctxEmpty, "test-exec-dead")
		if err == nil {
			t.Fatal("expected timeout when list is empty, got nil")
		}

		tsHealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"executorId": "test-exec-dead", "status": "HEALTHY"},
			})
		}))
		defer tsHealthy.Close()

		runnerHealthy := conformance.NewRunner(conformance.Config{
			TargetURL: tsHealthy.URL,
			OrgName:   "testorg",
			AppName:   "testapp",
		})
		ctxHealthy, cancelHealthy := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancelHealthy()

		err = conformance.WaitForExecutorUnhealthy(runnerHealthy, ctxHealthy, "test-exec-dead")
		if err == nil {
			t.Fatal("expected timeout when executor is HEALTHY, got nil")
		}
	})
}

func TestBattery7_RuleValidationFailures(t *testing.T) {
	t.Run("missing ID in rule creation fails check", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ruleType": "WorkflowFailure",
			})
		}))
		defer ts.Close()

		runner := conformance.NewRunner(conformance.Config{
			TargetURL: ts.URL,
			OrgName:   "testorg",
			AppName:   "testapp",
		})
		res := conformance.RunBattery7Alerting(runner, context.Background())
		if res.Status == conformance.StatusPass {
			t.Fatal("expected Battery 7 to fail when rule ID is missing from create response")
		}
	})
}

func TestRunner_TimeoutEnforcement(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer slowServer.Close()

	cfg := conformance.Config{
		TargetURL:      slowServer.URL,
		Timeout:        50 * time.Millisecond,
		SkipBatteryIDs: []int{2, 3, 4, 5, 6, 7, 8},
	}
	runner := conformance.NewRunner(cfg)
	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if report.AllPassed {
		t.Errorf("expected AllPassed = false when endpoint exceeds configured timeout")
	}
}
