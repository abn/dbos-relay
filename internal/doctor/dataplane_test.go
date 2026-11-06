package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/abn/relay/internal/protocol"
)

func TestAddSchemaCheckWindow(t *testing.T) {
	testCases := []struct {
		version int64
		want    Status
	}{
		{version: 122, want: StatusFail},
		{version: 123, want: StatusPass},
		{version: 125, want: StatusPass},
		{version: 126, want: StatusWarn},
	}
	for _, tc := range testCases {
		report := &Report{}
		addSchemaCheck(report, resolvedApp{Name: "orders"}, &schemaCapture{current: tc.version, seen: true})
		check := findCheck(t, report, "direct.schema[orders]")
		if check.Status != tc.want {
			t.Errorf("version %d status = %s, want %s (detail: %s)", tc.version, check.Status, tc.want, check.Detail)
		}
	}
}

func TestAddSchemaCheckUnknownVersionWarns(t *testing.T) {
	report := &Report{}
	addSchemaCheck(report, resolvedApp{Name: "orders"}, &schemaCapture{})
	check := findCheck(t, report, "direct.schema[orders]")
	if check.Status != StatusWarn {
		t.Fatalf("status = %s, want WARN", check.Status)
	}
	if !strings.Contains(check.Detail, "did not report the exact version") {
		t.Errorf("detail = %q, want an honest version gap", check.Detail)
	}
}

func TestScheduleFindings(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		app := "orders"
		dup, foreign, unowned := scheduleFindings(app, []protocol.ScheduleOutput{
			{ScheduleName: "nightly", ApplicationName: &app},
		})
		if len(dup) != 0 || len(foreign) != 0 || unowned != 0 {
			t.Fatalf("unexpected findings: dup=%v foreign=%v unowned=%d", dup, foreign, unowned)
		}
	})

	t.Run("duplicates and ownership", func(t *testing.T) {
		orders := "orders"
		other := "billing"
		dup, foreign, unowned := scheduleFindings(orders, []protocol.ScheduleOutput{
			{ScheduleName: "nightly", ApplicationName: &orders},
			{ScheduleName: "nightly", ApplicationName: &orders},
			{ScheduleName: "monthly", ApplicationName: &other},
			{ScheduleName: "legacy"},
		})
		if len(dup) != 1 || dup[0] != "nightly" {
			t.Errorf("duplicates = %v, want [nightly]", dup)
		}
		if len(foreign) != 1 || foreign[0] != "monthly" {
			t.Errorf("foreign = %v, want [monthly]", foreign)
		}
		if unowned != 1 {
			t.Errorf("unowned = %d, want 1", unowned)
		}
	})
}

func TestParseSchemaError(t *testing.T) {
	err := errors.New("failed to create dbos client: system database orders is at schema version 120, but this version of DBOS requires 123")
	current, required, ok := parseSchemaError(err)
	if !ok || current != 120 || required != 123 {
		t.Fatalf("parseSchemaError = (%d, %d, %t), want (120, 123, true)", current, required, ok)
	}
	if _, _, ok := parseSchemaError(errors.New("connection refused")); ok {
		t.Fatal("expected a non-schema error not to parse")
	}
}

func TestSanitizeError(t *testing.T) {
	err := errors.New(`failed to connect to postgres://relay:super-secret-password@db.internal:5432/orders?password=also-secret: connection refused`)
	got := SanitizeError(err)
	if strings.Contains(got, "super-secret-password") || strings.Contains(got, "also-secret") {
		t.Fatalf("sanitized error still contains a secret: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("sanitized error = %q, want a redaction marker", got)
	}
}
