package cli

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/abn/relay/internal/testdb"
)

func TestMigrateSubcommandsExist(t *testing.T) {
	cmd := newRootCommand()
	migrateCmd, _, err := cmd.Find([]string{"migrate"})
	if err != nil || migrateCmd == nil {
		t.Fatalf("expected migrate command to exist: %v", err)
	}

	subcommands := []string{"status", "force", "down", "up"}
	for _, sub := range subcommands {
		found, _, err := migrateCmd.Find([]string{sub})
		if err != nil || found == nil || found.Name() != sub {
			t.Errorf("expected migrate subcommand %q to exist", sub)
		}
	}
}

func TestMigrateForceRequiresConfirm(t *testing.T) {
	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"migrate", "force", "--version", "1"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected migrate force to fail without --confirm, got nil")
	}
	if !strings.Contains(err.Error(), "requires --confirm") {
		t.Fatalf("expected error mentioning --confirm, got: %v", err)
	}
}

func TestMigrateCLIIntegration(t *testing.T) {
	dbURL := testdb.OpenStore(t, "cli_migrate")

	t.Run("migrate up", func(t *testing.T) {
		cmd := newMigrateCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"up", "--database-url", dbURL})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("migrate up failed: %v", err)
		}
	})

	t.Run("migrate status", func(t *testing.T) {
		cmd := newMigrateCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"status", "--database-url", dbURL})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("migrate status failed: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "version=") || !strings.Contains(out, "dirty=") {
			t.Fatalf("unexpected status output: %q", out)
		}
	})

	t.Run("migrate down and force", func(t *testing.T) {
		// Read the current schema version so the round-trip below
		// stays valid as migrations are added.
		cmdVer := newMigrateCommand()
		bufVer := new(bytes.Buffer)
		cmdVer.SetOut(bufVer)
		cmdVer.SetErr(bufVer)
		cmdVer.SetArgs([]string{"status", "--database-url", dbURL})

		if err := cmdVer.Execute(); err != nil {
			t.Fatalf("migrate status failed: %v", err)
		}
		var current int
		if _, err := fmt.Sscanf(bufVer.String(), "version=%d dirty=false", &current); err != nil || current < 1 {
			t.Fatalf("unexpected status output: %q", bufVer.String())
		}

		cmdDown := newMigrateCommand()
		bufDown := new(bytes.Buffer)
		cmdDown.SetOut(bufDown)
		cmdDown.SetErr(bufDown)
		cmdDown.SetArgs([]string{"down", "--steps", "1", "--database-url", dbURL})

		if err := cmdDown.Execute(); err != nil {
			t.Fatalf("migrate down failed: %v", err)
		}

		cmdForce := newMigrateCommand()
		bufForce := new(bytes.Buffer)
		cmdForce.SetOut(bufForce)
		cmdForce.SetErr(bufForce)
		cmdForce.SetArgs([]string{"force", "--version", strconv.Itoa(current - 1), "--confirm", "--database-url", dbURL})

		if err := cmdForce.Execute(); err != nil {
			t.Fatalf("migrate force failed: %v", err)
		}

		// Re-apply migration up to restore the clean schema version
		cmdUp := newMigrateCommand()
		bufUp := new(bytes.Buffer)
		cmdUp.SetOut(bufUp)
		cmdUp.SetErr(bufUp)
		cmdUp.SetArgs([]string{"up", "--database-url", dbURL})

		if err := cmdUp.Execute(); err != nil {
			t.Fatalf("migrate up after force failed: %v", err)
		}

		// Verify status now shows the restored version dirty false
		cmdStatus := newMigrateCommand()
		bufStatus := new(bytes.Buffer)
		cmdStatus.SetOut(bufStatus)
		cmdStatus.SetErr(bufStatus)
		cmdStatus.SetArgs([]string{"status", "--database-url", dbURL})

		if err := cmdStatus.Execute(); err != nil {
			t.Fatalf("migrate status after force failed: %v", err)
		}
		if want := fmt.Sprintf("version=%d dirty=false", current); !strings.Contains(bufStatus.String(), want) {
			t.Fatalf("expected %s, got: %s", want, bufStatus.String())
		}
	})

	t.Run("verify triggers", func(t *testing.T) {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, dbURL)
		if err != nil {
			t.Fatalf("failed to connect to test db: %v", err)
		}
		defer func() { _ = conn.Close(ctx) }()

		var triggerCount int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal").Scan(&triggerCount); err != nil {
			t.Fatalf("failed to query triggers: %v", err)
		}
		if triggerCount < 2 {
			t.Fatalf("expected at least 2 non-internal triggers, got %d", triggerCount)
		}
	})
}
