package doctor

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/declarative"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store"
)

const (
	directSchemaFloor   int64 = 123
	directSchemaCeiling int64 = 125
)

type resolvedApp struct {
	Name             string
	URL              string
	Mode             dataplane.Mode
	Integration      dataplane.Integration
	StatementTimeout time.Duration
	MaxConnections   int
}

func runManifestChecks(ctx context.Context, report *Report, manifest *declarative.Config, loadErr error, s *store.Store) []resolvedApp {
	if loadErr != nil {
		report.Add(
			"manifest.load",
			StatusFail,
			"could not load the declarative manifest: "+SanitizeError(loadErr),
			"Fix the manifest path or YAML, then rerun doctor.",
		)
		return nil
	}
	if manifest == nil {
		report.Add(
			"manifest.load",
			StatusSkip,
			"no declarative manifest supplied.",
			"Set --file or RELAY_CONFIG to check data-plane and probe configuration.",
		)
		report.Add("data-plane.config", StatusSkip, "no manifest supplied.", "")
		return nil
	}

	report.Add(
		"manifest.load",
		StatusPass,
		fmt.Sprintf("loaded a manifest with %d application(s) and %d data-plane entry(s).", len(manifest.Applications), len(manifest.DataPlanes)),
		"",
	)

	declared := make(map[string]bool, len(manifest.Applications))
	for _, app := range manifest.Applications {
		declared[app.Name] = true
	}

	inControlPlane, queryErr := controlPlaneApplicationNames(ctx, s)
	canVerifyRegistration := queryErr == nil
	if queryErr != nil {
		report.Add(
			"manifest.registration",
			StatusWarn,
			"could not verify application registration in the control plane: "+SanitizeError(queryErr),
			"Fix the control-plane connection, then rerun doctor.",
		)
	}

	if len(manifest.DataPlanes) == 0 {
		report.Add("data-plane.config", StatusSkip, "the manifest has no data_plane entries.", "")
		return nil
	}

	var resolved []resolvedApp
	for _, name := range sortedKeys(manifest.DataPlanes) {
		dp := manifest.DataPlanes[name]
		integration := dataplane.Integration(dp.Integration)
		if integration == "" {
			integration = dataplane.IntegrationHybrid
		}
		mode := dataplane.Mode(dp.Mode)
		if mode == "" {
			mode = dataplane.ModeRead
		}

		addRegistrationCheck(report, name, declared[name], inControlPlane[name], canVerifyRegistration)

		checkID := "data-plane.manifest[" + name + "]"
		if integration == dataplane.IntegrationAttached {
			if dp.ConnectionURL != "" || dp.ConnectionStringFrom != nil {
				report.Add(
					checkID,
					StatusWarn,
					fmt.Sprintf("application %q uses Attached mode, so the configured database connection is ignored.", name),
					"Remove the data_plane connection or switch integration to hybrid or direct.",
				)
			} else {
				report.Add(checkID, StatusPass, fmt.Sprintf("application %q uses Attached mode and needs no database connection.", name), "")
			}
			continue
		}

		url, err := dp.ResolveConnectionURL()
		if err != nil {
			report.Add(
				checkID,
				StatusFail,
				"could not resolve the application database connection: "+SanitizeError(err),
				"Set the configured environment variable or secret file, then rerun doctor.",
			)
			continue
		}
		if strings.TrimSpace(url) == "" {
			report.Add(checkID, StatusFail, "the application database connection resolved to an empty value.", "Set a non-empty database URL.")
			continue
		}
		report.Add(
			checkID,
			StatusPass,
			fmt.Sprintf("application %q resolves a database connection (integration=%s, mode=%s).", name, integration, mode),
			"",
		)
		resolved = append(resolved, resolvedApp{
			Name:             name,
			URL:              url,
			Mode:             mode,
			Integration:      integration,
			StatementTimeout: time.Duration(dp.StatementTimeoutSecs) * time.Second,
			MaxConnections:   dp.MaxConnections,
		})
	}
	return resolved
}

func addRegistrationCheck(report *Report, appName string, declared, inControlPlane, canVerify bool) {
	checkID := "data-plane.registration[" + appName + "]"
	if !canVerify {
		report.Add(checkID, StatusSkip, "control-plane registration could not be verified.", "")
		return
	}
	switch {
	case declared && inControlPlane:
		report.Add(checkID, StatusPass, fmt.Sprintf("application %q is declared and present in the control plane.", appName), "")
	case declared:
		report.Add(
			checkID,
			StatusWarn,
			fmt.Sprintf("application %q is declared in the manifest but not yet in the control plane.", appName),
			"Run 'relay apply -f <manifest>' before starting Relay.",
		)
	case inControlPlane:
		report.Add(checkID, StatusPass, fmt.Sprintf("application %q exists in the control plane and is not managed by the manifest.", appName), "")
	default:
		report.Add(
			checkID,
			StatusFail,
			fmt.Sprintf("application %q is neither declared in the manifest nor present in the control plane.", appName),
			"Add the application to the manifest and run 'relay apply', or correct the data_plane key.",
		)
	}
}

func controlPlaneApplicationNames(ctx context.Context, s *store.Store) (map[string]bool, error) {
	names := make(map[string]bool)
	if s == nil {
		return names, fmt.Errorf("control-plane database is not available")
	}
	apps, err := s.Queries().ListAllApplications(ctx)
	if err != nil {
		return names, err
	}
	for _, app := range apps {
		names[app.Name] = true
	}
	return names, nil
}

func runDataPlaneChecks(ctx context.Context, report *Report, apps []resolvedApp, opts Options) {
	if len(apps) == 0 {
		report.Add("data-plane.connectivity", StatusSkip, "no non-Attached data-plane entries to check.", "")
		return
	}
	for _, app := range apps {
		checkDataPlane(ctx, report, app, opts)
		if app.Integration == dataplane.IntegrationDirect {
			report.Add(
				"rollback.application["+app.Name+"]",
				StatusSkip,
				"Relay cannot verify an application database snapshot or the prior control plane.",
				"Take an application system database snapshot and record the prior control plane before a Direct cutover.",
			)
		}
	}
}

func checkDataPlane(ctx context.Context, report *Report, app resolvedApp, opts Options) {
	statementTimeout := app.StatementTimeout
	if statementTimeout <= 0 {
		statementTimeout = opts.Timeout
	}
	maxConnections := app.MaxConnections
	if maxConnections <= 0 {
		maxConnections = 2
	}

	capture := &schemaCapture{}
	cfg := dataplane.AppConfig{
		ApplicationName:  app.Name,
		DatabaseURL:      app.URL,
		Mode:             app.Mode,
		Integration:      app.Integration,
		MaxConnections:   maxConnections,
		StatementTimeout: statementTimeout,
		Logger:           slog.New(capture),
	}
	client, err := dataplane.NewSDKClientWithContext(ctx, cfg)
	if err != nil {
		report.Add(
			"data-plane.connectivity["+app.Name+"]",
			StatusFail,
			"the SDK client rejected the application system database: "+SanitizeError(err),
			"Check the connection URL, network path, database role, and schema generation.",
		)
		if current, required, ok := parseSchemaError(err); ok {
			report.Add(
				"direct.schema["+app.Name+"]",
				StatusFail,
				fmt.Sprintf("schema version %d is below the %d required by the Direct client.", current, required),
				"Use Attached mode or migrate the application system database to schema 123 through 125 with its SDK.",
			)
			report.Add("data-plane.read["+app.Name+"]", StatusSkip, "the client could not connect.", "")
		} else {
			report.Add("direct.schema["+app.Name+"]", StatusSkip, "the client could not connect.", "")
			report.Add("data-plane.read["+app.Name+"]", StatusSkip, "the client could not connect.", "")
		}
		report.Add("data-plane.write["+app.Name+"]", StatusSkip, "the client could not connect.", "")
		report.Add("schedules["+app.Name+"]", StatusSkip, "the client could not connect.", "")
		return
	}
	defer func() {
		if err := client.Close(); err != nil {
			report.Add("data-plane.close["+app.Name+"]", StatusWarn, "closing the SDK client failed: "+SanitizeError(err), "")
		}
	}()

	report.Add(
		"data-plane.connectivity["+app.Name+"]",
		StatusPass,
		fmt.Sprintf("connected with integration=%s and mode=%s.", app.Integration, app.Mode),
		"",
	)
	addSchemaCheck(report, app, capture)
	addReadCheck(ctx, report, app, client)
	addDataPlaneWriteCheck(report, app)
	addScheduleCheck(ctx, report, app, client)
}

func addSchemaCheck(report *Report, app resolvedApp, capture *schemaCapture) {
	version, ok := capture.version()
	if !ok {
		report.Add(
			"direct.schema["+app.Name+"]",
			StatusWarn,
			"the client verified the schema floor but did not report the exact version.",
			"Confirm the application system database is within the supported window of 123 through 125 before production.",
		)
		return
	}

	switch {
	case version < directSchemaFloor:
		report.Add(
			"direct.schema["+app.Name+"]",
			StatusFail,
			fmt.Sprintf("schema version %d is below the supported window 123 through 125.", version),
			"Use Attached mode or migrate the application system database with its SDK.",
		)
	case version > directSchemaCeiling:
		report.Add(
			"direct.schema["+app.Name+"]",
			StatusWarn,
			fmt.Sprintf("schema version %d is newer than the tested window 123 through 125.", version),
			"The client tolerates newer schemas, but Relay has not tested this generation. Validate reads and mutations in staging.",
		)
	default:
		report.Add(
			"direct.schema["+app.Name+"]",
			StatusPass,
			fmt.Sprintf("schema version %d is inside the supported window 123 through 125.", version),
			"",
		)
	}
}

func addReadCheck(ctx context.Context, report *Report, app resolvedApp, client dataplane.Client) {
	limit := 1
	resp, err := client.Dispatch(ctx, &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListWorkflows,
			RequestID: "doctor-read",
		},
		Body: protocol.ListWorkflowsRequestBody{
			ApplicationName: protocol.StringOrList{app.Name},
			Limit:           &limit,
		},
	})
	if err != nil {
		report.Add(
			"data-plane.read["+app.Name+"]",
			StatusFail,
			"a read query failed: "+SanitizeError(err),
			"Grant the database role read access to the application system tables.",
		)
		return
	}
	if _, ok := resp.(*protocol.ListWorkflowsResponse); !ok {
		report.Add(
			"data-plane.read["+app.Name+"]",
			StatusFail,
			"the SDK client returned an unexpected response type.",
			"Check the Relay data-plane implementation against the pinned SDK client.",
		)
		return
	}
	report.Add("data-plane.read["+app.Name+"]", StatusPass, "a bounded workflow read succeeded.", "")
}

func addDataPlaneWriteCheck(report *Report, app resolvedApp) {
	switch app.Mode {
	case dataplane.ModeRead:
		report.Add(
			"data-plane.write["+app.Name+"]",
			StatusPass,
			"read-only mode is configured, so write access is not required.",
			"",
		)
	case dataplane.ModeReadWrite:
		report.Add(
			"data-plane.write["+app.Name+"]",
			StatusSkip,
			"Relay has no non-destructive write probe for an application system database.",
			"Verify read-write mode with one safe mutation in staging before production.",
		)
	default:
		report.Add(
			"data-plane.write["+app.Name+"]",
			StatusFail,
			fmt.Sprintf("unknown data-plane mode %q.", app.Mode),
			"Use 'read' or 'read-write' in the manifest.",
		)
	}
}

func addScheduleCheck(ctx context.Context, report *Report, app resolvedApp, client dataplane.Client) {
	loadContext := false
	resp, err := client.Dispatch(ctx, &protocol.ListSchedulesRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListSchedules,
			RequestID: "doctor-schedules",
		},
		Body: protocol.ListSchedulesRequestBody{
			ApplicationName: protocol.StringOrList{app.Name},
			LoadContext:     &loadContext,
		},
	})
	if err != nil {
		report.Add(
			"schedules["+app.Name+"]",
			StatusFail,
			"listing schedules failed: "+SanitizeError(err),
			"Grant the database role read access to the schedule tables.",
		)
		return
	}
	schedules, ok := resp.(*protocol.ListSchedulesResponse)
	if !ok {
		report.Add(
			"schedules["+app.Name+"]",
			StatusFail,
			"the SDK client returned an unexpected schedule response type.",
			"Check the Relay data-plane implementation against the pinned SDK client.",
		)
		return
	}

	duplicates, foreign, unowned := scheduleFindings(app.Name, schedules.Output)
	var details []string
	if len(duplicates) > 0 {
		details = append(details, "duplicate schedule names: "+strings.Join(duplicates, ", "))
	}
	if len(foreign) > 0 {
		details = append(details, "schedules owned by another application: "+strings.Join(foreign, ", "))
	}
	if unowned > 0 {
		details = append(details, fmt.Sprintf("%d schedule row(s) have no recorded application owner", unowned))
	}

	switch {
	case len(duplicates) > 0:
		report.Add(
			"schedules["+app.Name+"]",
			StatusFail,
			"found "+strings.Join(details, "; ")+".",
			"Remove duplicate schedule rows before Direct mode uses this application; schedule names must be unique.",
		)
	case len(details) > 0:
		report.Add(
			"schedules["+app.Name+"]",
			StatusWarn,
			"found "+strings.Join(details, "; ")+".",
			"Confirm the application name matches the DBOS app_name and review legacy schedule ownership.",
		)
	default:
		report.Add(
			"schedules["+app.Name+"]",
			StatusPass,
			fmt.Sprintf("%d schedule(s) have unique names and the expected application owner.", len(schedules.Output)),
			"",
		)
	}
}

// scheduleFindings reports duplicate names, schedules owned by another
// application, and rows with no owner. It is a pure function so the ownership
// rules can be tested without an application database.
func scheduleFindings(appName string, schedules []protocol.ScheduleOutput) (duplicates, foreign []string, unowned int) {
	counts := make(map[string]int)
	for _, schedule := range schedules {
		counts[schedule.ScheduleName]++
		if schedule.ApplicationName == nil || *schedule.ApplicationName == "" {
			unowned++
			continue
		}
		if *schedule.ApplicationName != appName {
			foreign = append(foreign, schedule.ScheduleName)
		}
	}
	for name, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, name)
		}
	}
	sort.Strings(duplicates)
	sort.Strings(foreign)
	return duplicates, foreign, unowned
}

var schemaErrorPattern = regexp.MustCompile(`(?i)is at schema version (\d+), but this version of DBOS requires (\d+)`)

func parseSchemaError(err error) (current, required int64, ok bool) {
	if err == nil {
		return 0, 0, false
	}
	match := schemaErrorPattern.FindStringSubmatch(err.Error())
	if match == nil {
		return 0, 0, false
	}
	current, err1 := strconv.ParseInt(match[1], 10, 64)
	required, err2 := strconv.ParseInt(match[2], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return current, required, true
}

// schemaCapture reads the schema version the SDK client logs after it verifies
// an application system database.
//
// Provenance: dbos-inc/dbos-transact-golang v1.7.0,
// dbos/internal/sysdb/system_database.go:725 and
// dbos/internal/sysdb/sqlite_migrations.go:288. The client has no exported
// schema version accessor, so this is the only non-SQL path.
type schemaCapture struct {
	mu       sync.Mutex
	current  int64
	required int64
	seen     bool
}

func (c *schemaCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *schemaCapture) Handle(_ context.Context, record slog.Record) error {
	if record.Message != "System database schema version satisfies the required version" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	record.Attrs(func(attr slog.Attr) bool {
		switch attr.Key {
		case "current_version":
			if attr.Value.Kind() == slog.KindInt64 {
				c.current = attr.Value.Int64()
				c.seen = true
			}
		case "required_version":
			if attr.Value.Kind() == slog.KindInt64 {
				c.required = attr.Value.Int64()
			}
		}
		return true
	})
	return nil
}

func (c *schemaCapture) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c *schemaCapture) WithGroup(string) slog.Handler { return c }

func (c *schemaCapture) version() (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current, c.seen
}
