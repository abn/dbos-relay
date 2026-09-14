package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
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
	dbURL := os.Getenv("RELAY_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL not set; skipping migrate CLI integration test")
	}

	t.Run("migrate up", func(t *testing.T) {
		cmd := newRootCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"migrate", "up", "--database-url", dbURL})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("migrate up failed: %v", err)
		}
	})

	t.Run("migrate status", func(t *testing.T) {
		cmd := newRootCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"migrate", "status", "--database-url", dbURL})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("migrate status failed: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "version=") || !strings.Contains(out, "dirty=") {
			t.Fatalf("unexpected status output: %q", out)
		}
	})

	t.Run("migrate down and force", func(t *testing.T) {
		cmdDown := newRootCommand()
		bufDown := new(bytes.Buffer)
		cmdDown.SetOut(bufDown)
		cmdDown.SetErr(bufDown)
		cmdDown.SetArgs([]string{"migrate", "down", "--steps", "1", "--database-url", dbURL})

		if err := cmdDown.Execute(); err != nil {
			t.Fatalf("migrate down failed: %v", err)
		}

		cmdForce := newRootCommand()
		bufForce := new(bytes.Buffer)
		cmdForce.SetOut(bufForce)
		cmdForce.SetErr(bufForce)
		cmdForce.SetArgs([]string{"migrate", "force", "--version", "5", "--confirm", "--database-url", dbURL})

		if err := cmdForce.Execute(); err != nil {
			t.Fatalf("migrate force failed: %v", err)
		}

		// Verify status now shows version 5 dirty false
		cmdStatus := newRootCommand()
		bufStatus := new(bytes.Buffer)
		cmdStatus.SetOut(bufStatus)
		cmdStatus.SetErr(bufStatus)
		cmdStatus.SetArgs([]string{"migrate", "status", "--database-url", dbURL})

		if err := cmdStatus.Execute(); err != nil {
			t.Fatalf("migrate status after force failed: %v", err)
		}
		if !strings.Contains(bufStatus.String(), "version=5 dirty=false") {
			t.Fatalf("expected version=5 dirty=false, got: %s", bufStatus.String())
		}
	})
}
