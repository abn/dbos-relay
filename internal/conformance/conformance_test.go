package conformance_test

import (
	"bytes"
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
