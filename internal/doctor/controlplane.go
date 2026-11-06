package doctor

import (
	"context"
	"fmt"

	"github.com/abn/relay/internal/store"
)

func runControlPlaneChecks(ctx context.Context, s *store.Store, report *Report) {
	report.Add("control-plane.connectivity", StatusPass, "connected to the control-plane database.", "")

	if _, err := s.Queries().ListAllApplications(ctx); err != nil {
		report.Add(
			"control-plane.read",
			StatusFail,
			"connected but a read query failed: "+SanitizeError(err),
			"Grant the Relay database role read access to the control-plane schema.",
		)
	} else {
		report.Add("control-plane.read", StatusPass, "read access to control-plane tables verified.", "")
	}

	status, err := s.MigrationStatus(ctx)
	if err != nil {
		report.Add(
			"control-plane.migrations",
			StatusFail,
			"could not read the migration state: "+SanitizeError(err),
			"Check the migration table and the database role's access to it.",
		)
		report.Add("control-plane.write", StatusSkip, "migration state is unavailable.", "")
		report.Add("rollback.control-plane", StatusSkip, "migration state is unavailable.", "")
		report.Add("rollback.snapshot", StatusSkip, "Relay cannot verify that a database snapshot exists. Take one before migrating or rolling back.", "")
		return
	}

	addMigrationCheck(report, status)
	addControlPlaneWriteCheck(ctx, s, report, status)
	addRollbackCheck(report, status)
}

func addMigrationCheck(report *Report, status store.MigrationStatus) {
	switch {
	case status.Dirty:
		report.Add(
			"control-plane.migrations",
			StatusFail,
			fmt.Sprintf("schema is dirty at version %d; Relay refuses to start or migrate until it is resolved.", status.Version),
			fmt.Sprintf("Inspect the database, then run 'relay migrate force --version %d --confirm' after resolving the failed migration.", status.Version),
		)
	case status.Version == 0:
		report.Add(
			"control-plane.migrations",
			StatusWarn,
			"no control-plane migration has been applied.",
			"Run 'relay migrate up' before starting Relay in production.",
		)
	case status.Version < status.Latest:
		report.Add(
			"control-plane.migrations",
			StatusWarn,
			fmt.Sprintf("schema version %d is behind the %d embedded in this binary.", status.Version, status.Latest),
			"Run 'relay migrate up' before routing production traffic.",
		)
	case status.Version > status.Latest:
		report.Add(
			"control-plane.migrations",
			StatusFail,
			fmt.Sprintf("schema version %d is newer than the %d embedded in this binary.", status.Version, status.Latest),
			"Use a Relay binary that embeds this schema version; an older binary may refuse to start.",
		)
	default:
		report.Add(
			"control-plane.migrations",
			StatusPass,
			fmt.Sprintf("schema version %d is current and clean.", status.Version),
			"",
		)
	}
}

func addControlPlaneWriteCheck(ctx context.Context, s *store.Store, report *Report, status store.MigrationStatus) {
	if status.Version == 0 {
		report.Add("control-plane.write", StatusSkip, "the control-plane schema is not initialised.", "Run 'relay migrate up' first.")
		return
	}
	if err := s.ProbeWrite(ctx); err != nil {
		report.Add(
			"control-plane.write",
			StatusFail,
			"write access check failed: "+SanitizeError(err),
			"Grant the Relay database role write access to the control-plane tables.",
		)
		return
	}
	report.Add("control-plane.write", StatusPass, "write access verified with a rolled-back no-op update.", "")
}

func addRollbackCheck(report *Report, status store.MigrationStatus) {
	switch {
	case status.Dirty:
		report.Add(
			"rollback.control-plane",
			StatusFail,
			fmt.Sprintf("schema is dirty at version %d; rollback is blocked until the failed migration is resolved.", status.Version),
			"Resolve the failed migration and clear the dirty state before rolling back.",
		)
	case status.Version == 0:
		report.Add("rollback.control-plane", StatusWarn, "no applied migrations, so there is nothing to roll back.", "")
	case status.Version > status.Latest:
		report.Add(
			"rollback.control-plane",
			StatusFail,
			fmt.Sprintf("schema version %d is ahead of this binary; its down migration is not embedded.", status.Version),
			"Use the binary that applied the schema, or restore from a snapshot.",
		)
	case !status.DownAvailable:
		report.Add(
			"rollback.control-plane",
			StatusFail,
			fmt.Sprintf("no paired down migration is embedded for version %d.", status.Version),
			"Restore from a database snapshot instead of running a down migration.",
		)
	default:
		report.Add(
			"rollback.control-plane",
			StatusPass,
			fmt.Sprintf("the down migration for version %d is embedded and the schema is clean.", status.Version),
			"",
		)
	}

	report.Add(
		"rollback.snapshot",
		StatusSkip,
		"Relay cannot verify that a database snapshot exists.",
		"Take and test a control-plane database snapshot before migrating or rolling back.",
	)
}

func skipControlPlaneChecks(report *Report) {
	report.Add("control-plane.read", StatusSkip, "the control-plane database is not reachable.", "")
	report.Add("control-plane.migrations", StatusSkip, "the control-plane database is not reachable.", "")
	report.Add("control-plane.write", StatusSkip, "the control-plane database is not reachable.", "")
	report.Add("rollback.control-plane", StatusSkip, "the control-plane database is not reachable.", "")
	report.Add("rollback.snapshot", StatusSkip, "Relay cannot verify that a database snapshot exists. Take one before migrating or rolling back.", "")
}
