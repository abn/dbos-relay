package verifysdk_test

import (
	"strings"
	"testing"
	"time"
)

func TestReportMarkdown_DynamicResults(t *testing.T) {
	containers := map[string]containerInfo{
		"Python":     {Name: "proc-py", AppName: "app-py", ExecutorID: "exec-py", AppVersion: "1.0.0"},
		"TypeScript": {Name: "proc-ts", AppName: "app-ts", ExecutorID: "exec-ts", AppVersion: "1.0.0"},
		"Go":         {Name: "proc-go", AppName: "app-go", ExecutorID: "exec-go", AppVersion: "1.0.0"},
		"Java":       {Name: "proc-java", AppName: "app-java", ExecutorID: "exec-java", AppVersion: "1.0.0"},
	}

	cellResults := make(map[int]map[string]CellResult)
	for i := 1; i <= 7; i++ {
		cellResults[i] = make(map[string]CellResult)
		for _, lang := range []string{"Python", "TypeScript", "Go", "Java"} {
			cellResults[i][lang] = CellResult{Status: CellStatusPass}
		}
	}

	// Dynamic modification 1: Cell 3 Python fails
	cellResults[3]["Python"] = CellResult{Status: CellStatusFail, Reason: "status 500"}

	// Dynamic modification 2: Cell 4 Java skipped
	cellResults[4]["Java"] = CellResult{Status: CellStatusSkip, Reason: "upstream schema v19 vs v107"}

	// Dynamic modification 3: Cell 5 Go is an unexecuted gap
	delete(cellResults[5], "Go")

	cellDurations := map[string]time.Duration{
		"Cell 1: Socket connection":          100 * time.Millisecond,
		"Cell 2: Conformance and REST probes": 200 * time.Millisecond,
		"Cell 3: Data plane read":            300 * time.Millisecond,
		"Cell 4: Field parity":       400 * time.Millisecond,
		"Cell 5: Chaos and recovery": 500 * time.Millisecond,
		"Cell 6: Offline cancel/resume": 600 * time.Millisecond,
		"Cell 7: Data-plane fork":    700 * time.Millisecond,
	}

	report := generateReportMarkdown(containers, cellResults, cellDurations, 2800*time.Millisecond, "podman ps output mock")

	// Verify header
	if !strings.Contains(report, "| Cell | Python | TypeScript | Go | Java | Duration |") {
		t.Fatalf("missing or malformed table header in report:\n%s", report)
	}

	// Verify Cell 3 shows FAIL for Python and PASS for others
	expectedCell3 := "| 3. Data plane read and preservation | FAIL: status 500 | PASS | PASS | PASS | 300ms |"
	if !strings.Contains(report, expectedCell3) {
		t.Errorf("expected report to contain line:\n%s\ngot report:\n%s", expectedCell3, report)
	}

	// Verify Cell 4 shows SKIPPED for Java
	expectedCell4 := "| 4. Field parity between socket and database | PASS | PASS | PASS | [SKIPPED: upstream schema v19 vs v107] | 400ms |"
	if !strings.Contains(report, expectedCell4) {
		t.Errorf("expected report to contain line:\n%s\ngot report:\n%s", expectedCell4, report)
	}

	// Verify Cell 5 shows GAP for Go
	expectedCell5 := "| 5. Chaos, real timers, and recovery | PASS | PASS | [GAP: not executed] | PASS | 500ms |"
	if !strings.Contains(report, expectedCell5) {
		t.Errorf("expected report to contain line:\n%s\ngot report:\n%s", expectedCell5, report)
	}
}
