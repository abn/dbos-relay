// Package doctor runs read-only preflight checks for a Relay deployment.
//
// Doctor never applies migrations, writes to an application system database,
// or prints credentials. Checks that cannot be made safely are reported as
// skipped instead of being simulated.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/abn/relay/internal/declarative"
	"github.com/abn/relay/internal/store"
)

// Status is the outcome of a single check.
type Status string

const (
	// StatusPass means the check completed and found no problem.
	StatusPass Status = "PASS"
	// StatusWarn means the deployment works but needs attention before
	// production, or a precondition could not be verified.
	StatusWarn Status = "WARN"
	// StatusFail means the deployment is not ready and a check must be fixed.
	StatusFail Status = "FAIL"
	// StatusSkip means the check could not be performed safely or does not
	// apply to this configuration.
	StatusSkip Status = "SKIP"
)

// Check is one preflight result.
type Check struct {
	ID     string
	Status Status
	Detail string
	Remedy string
}

// Report is the ordered set of checks from one doctor run.
type Report struct {
	Checks []Check
}

// Add appends a check to the report.
func (r *Report) Add(id string, status Status, detail, remedy string) {
	r.Checks = append(r.Checks, Check{
		ID:     id,
		Status: status,
		Detail: detail,
		Remedy: remedy,
	})
}

// Passed reports the number of passing checks.
func (r *Report) Passed() int { return r.count(StatusPass) }

// Warnings reports the number of warnings.
func (r *Report) Warnings() int { return r.count(StatusWarn) }

// Failed reports the number of failing checks.
func (r *Report) Failed() int { return r.count(StatusFail) }

// Skipped reports the number of skipped checks.
func (r *Report) Skipped() int { return r.count(StatusSkip) }

func (r *Report) count(status Status) int {
	var n int
	for _, check := range r.Checks {
		if check.Status == status {
			n++
		}
	}
	return n
}

// String renders the report for a terminal.
func (r *Report) String() string {
	if len(r.Checks) == 0 {
		return "Relay doctor\n\nNo checks ran.\n"
	}

	width := 0
	for _, check := range r.Checks {
		if len(check.ID) > width {
			width = len(check.ID)
		}
	}

	var b strings.Builder
	b.WriteString("Relay doctor\n\n")
	for _, check := range r.Checks {
		fmt.Fprintf(&b, "%-4s  %-*s  %s\n", check.Status, width, check.ID, check.Detail)
		if check.Remedy != "" {
			fmt.Fprintf(&b, "      %-*s  remedy: %s\n", width, "", check.Remedy)
		}
	}
	fmt.Fprintf(&b, "\nSummary: %d passed, %d warning(s), %d failed, %d skipped.\n",
		r.Passed(), r.Warnings(), r.Failed(), r.Skipped())
	return b.String()
}

// FailureError is returned by the command when one or more checks fail. It
// gives the process exit code 1 without hiding the rendered report.
type FailureError struct {
	Failed int
}

func (e *FailureError) Error() string {
	return fmt.Sprintf("doctor found %d failing check(s)", e.Failed)
}

// ExitCode reports the process exit code for a failed preflight.
func (e *FailureError) ExitCode() int { return 1 }

// UsageError reports that doctor could not start because its inputs were
// incomplete or invalid. It gives the process exit code 2.
type UsageError struct {
	Err error
}

func (e *UsageError) Error() string { return e.Err.Error() }

// Unwrap exposes the underlying error.
func (e *UsageError) Unwrap() error { return e.Err }

// ExitCode reports the process exit code for an unusable invocation.
func (e *UsageError) ExitCode() int { return 2 }

// NewUsageError wraps err so the command exits with code 2.
func NewUsageError(err error) error {
	if err == nil {
		return nil
	}
	return &UsageError{Err: err}
}

// Options configures a doctor run.
type Options struct {
	// DatabaseURL points at Relay's control-plane database.
	DatabaseURL string
	// ManifestPath is the optional declarative manifest. When empty, doctor
	// reads RELAY_CONFIG through Env.
	ManifestPath string
	// RelayURL is the base URL of a running Relay instance. When empty,
	// doctor reads RELAY_PROBE_URL, then RELAY_URL, then uses
	// http://127.0.0.1:8090.
	RelayURL string
	// ProbeApp is the probe application identity. When empty, doctor reads
	// DBOS_APP_NAME.
	ProbeApp string
	// ProbeExecutorID is the probe executor identity. When empty, doctor
	// reads DBOS__VMID.
	ProbeExecutorID string
	// Env reads environment variables. A nil value uses os.Getenv.
	Env func(string) string
	// HTTPClient checks the probe endpoint. A nil value uses a short-timeout
	// client.
	HTTPClient *http.Client
	// Timeout bounds each network operation. A non-positive value uses 10s.
	Timeout time.Duration
}

// Run executes every safe check and returns a report. It returns an error only
// when the invocation cannot start, never for a failed check.
func Run(ctx context.Context, opts Options) (*Report, error) {
	if strings.TrimSpace(opts.DatabaseURL) == "" {
		return nil, errors.New("control-plane database URL is required (set --database-url, --embedded, or RELAY_DATABASE_URL)")
	}
	if opts.Env == nil {
		opts.Env = os.Getenv
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.Timeout}
	}

	report := &Report{}

	s, err := store.Open(ctx, opts.DatabaseURL)
	if err != nil {
		report.Add(
			"control-plane.connectivity",
			StatusFail,
			"could not connect to the control-plane database: "+SanitizeError(err),
			"Check --database-url or RELAY_DATABASE_URL, the database host, and the credentials.",
		)
		skipControlPlaneChecks(report)
	} else {
		defer s.Close()
		runControlPlaneChecks(ctx, s, report)
	}

	manifest, manifestErr := loadManifest(opts)
	resolved := runManifestChecks(ctx, report, manifest, manifestErr, s)
	runDataPlaneChecks(ctx, report, resolved, opts)
	runProbeChecks(ctx, report, manifest, opts)

	return report, nil
}

func loadManifest(opts Options) (*declarative.Config, error) {
	path := strings.TrimSpace(opts.ManifestPath)
	if path == "" {
		path = strings.TrimSpace(opts.Env("RELAY_CONFIG"))
	}
	if path == "" {
		return nil, nil
	}
	return declarative.LoadFile(path)
}

// sortedKeys returns map keys in stable order so reports and tests do not
// depend on map iteration.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
