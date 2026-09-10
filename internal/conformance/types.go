package conformance

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// BatteryStatus represents the outcome of a conformance check or battery.
type BatteryStatus string

const (
	StatusPass BatteryStatus = "PASS"
	StatusFail BatteryStatus = "FAIL"
	StatusSkip BatteryStatus = "SKIP"
)

// CheckResult records the outcome of a single check within a battery.
type CheckResult struct {
	Name    string        `json:"name"`
	Status  BatteryStatus `json:"status"`
	Detail  string        `json:"detail,omitempty"`
	Elapsed time.Duration `json:"elapsed"`
}

// BatteryResult records the outcome of an entire battery.
type BatteryResult struct {
	ID      int           `json:"id"`
	Title   string        `json:"title"`
	Status  BatteryStatus `json:"status"`
	Checks  []CheckResult `json:"checks"`
	Elapsed time.Duration `json:"elapsed"`
	Error   string        `json:"error,omitempty"`
}

// Report holds the complete results of a conformance test run.
type Report struct {
	TargetURL string          `json:"target_url"`
	Timestamp time.Time       `json:"timestamp"`
	Batteries []BatteryResult `json:"batteries"`
	TotalPass int             `json:"total_pass"`
	TotalFail int             `json:"total_fail"`
	TotalSkip int             `json:"total_skip"`
	AllPassed bool            `json:"all_passed"`
	Duration  time.Duration   `json:"duration"`
}

// FormatMarkdown outputs a clean, readable conformance scorecard in Markdown.
func (r *Report) FormatMarkdown(w io.Writer) error {
	var b strings.Builder
	b.WriteString("# Conformance Test Suite Scorecard\n\n")
	b.WriteString(fmt.Sprintf("- **Target URL**: `%s`\n", r.TargetURL))
	b.WriteString(fmt.Sprintf("- **Timestamp**: %s\n", r.Timestamp.UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- **Duration**: %s\n", r.Duration.Round(time.Millisecond)))
	if r.AllPassed {
		b.WriteString("- **Overall Status**: ✅ **CONFORMANT (100% PASS)**\n\n")
	} else {
		b.WriteString(fmt.Sprintf("- **Overall Status**: ❌ **NON-CONFORMANT (%d failed)**\n\n", r.TotalFail))
	}

	b.WriteString("| Battery | Title | Status | Checks | Duration |\n")
	b.WriteString("| :---: | :--- | :---: | :---: | :---: |\n")

	for _, bat := range r.Batteries {
		statusIcon := "✅"
		switch bat.Status {
		case StatusFail:
			statusIcon = "❌"
		case StatusSkip:
			statusIcon = "⚪"
		}

		passedChecks := 0
		for _, c := range bat.Checks {
			if c.Status == StatusPass {
				passedChecks++
			}
		}

		b.WriteString(fmt.Sprintf("| **B%d** | %s | %s %s | %d/%d | %s |\n",
			bat.ID,
			bat.Title,
			statusIcon,
			bat.Status,
			passedChecks,
			len(bat.Checks),
			bat.Elapsed.Round(time.Millisecond),
		))
	}

	b.WriteString("\n## Check Details\n\n")
	for _, bat := range r.Batteries {
		b.WriteString(fmt.Sprintf("### Battery %d: %s\n\n", bat.ID, bat.Title))
		if bat.Error != "" {
			b.WriteString(fmt.Sprintf("> [!WARNING]\n> **Battery Error**: %s\n\n", bat.Error))
		}
		b.WriteString("| Check | Status | Elapsed | Detail |\n")
		b.WriteString("| :--- | :---: | :---: | :--- |\n")
		for _, c := range bat.Checks {
			icon := "✅"
			switch c.Status {
			case StatusFail:
				icon = "❌"
			case StatusSkip:
				icon = "⚪"
			}
			detail := c.Detail
			if detail == "" {
				detail = "-"
			}
			b.WriteString(fmt.Sprintf("| %s | %s %s | %s | %s |\n",
				c.Name,
				icon,
				c.Status,
				c.Elapsed.Round(time.Millisecond),
				detail,
			))
		}
		b.WriteString("\n")
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// Config specifies the runtime options for a conformance test run.
type Config struct {
	TargetURL      string
	ConductorKey   string
	OrgName        string
	AppName        string
	Output         io.Writer
	Verbose        bool
	Timeout        time.Duration
	SkipBatteryIDs []int
	SkipReason     string
}
