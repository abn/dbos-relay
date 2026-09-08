package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCommandTreeSubcommandsExist(t *testing.T) {
	cmd := newRootCommand()
	expected := []string{"version", "serve", "migrate", "apikey", "openapi", "test-conformance"}
	for _, name := range expected {
		sub, _, err := cmd.Find([]string{name})
		if err != nil || sub == nil || sub.Name() != name {
			t.Errorf("expected subcommand %q to exist on root command", name)
		}
	}
}

func TestServeRequiresDatabaseURL(t *testing.T) {
	t.Setenv("RELAY_DATABASE_URL", "")

	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"serve"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected serve to fail when RELAY_DATABASE_URL is unset, got nil")
	}
	if !strings.Contains(err.Error(), "RELAY_DATABASE_URL") {
		t.Fatalf("expected error to mention RELAY_DATABASE_URL, got: %v", err)
	}
}

func TestOpenAPICommand(t *testing.T) {
	t.Run("default json", func(t *testing.T) {
		cmd := newRootCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"openapi"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error executing openapi: %v", err)
		}

		out := buf.String()
		if !strings.Contains(out, "openapi") {
			t.Fatalf("expected output to contain 'openapi', got: %s", out)
		}

		var parsed map[string]any
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("expected output to parse as valid JSON: %v", err)
		}
	})

	t.Run("yaml flag", func(t *testing.T) {
		cmd := newRootCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"openapi", "--yaml"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error executing openapi --yaml: %v", err)
		}

		var parsed any
		if err := yaml.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("expected output to parse as valid YAML: %v", err)
		}
	})

	t.Run("version 3.0", func(t *testing.T) {
		cmd := newRootCommand()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"openapi", "--version", "3.0"})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error executing openapi --version 3.0: %v", err)
		}

		var parsed map[string]any
		if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
			t.Fatalf("expected output to parse as valid JSON: %v", err)
		}
		if v, ok := parsed["openapi"].(string); !ok || !strings.HasPrefix(v, "3.0") {
			t.Fatalf("expected openapi 3.0.x, got %v", parsed["openapi"])
		}
	})
}

func TestAPIKeyCreateRequiresFlags(t *testing.T) {
	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "missing both flags",
			args: []string{"apikey", "create"},
		},
		{
			name: "missing name flag",
			args: []string{"apikey", "create", "--org", "acme"},
		},
		{
			name: "missing org flag",
			args: []string{"apikey", "create", "--name", "laptop"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCommand()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected command with args %v to fail, got nil", tc.args)
			}
		})
	}
}

func TestVersionCommand(t *testing.T) {
	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing version: %v", err)
	}
	if !strings.Contains(buf.String(), Version) {
		t.Fatalf("expected output to contain %q, got: %q", Version, buf.String())
	}
}

func TestMigrateRequiresDatabaseURL(t *testing.T) {
	t.Setenv("RELAY_DATABASE_URL", "")

	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"migrate"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected migrate to fail when RELAY_DATABASE_URL is unset, got nil")
	}
	if !strings.Contains(err.Error(), "database URL is required") {
		t.Fatalf("expected error mentioning database URL is required, got: %v", err)
	}
}

func TestOpenAPICommandInvalidVersion(t *testing.T) {
	cmd := newRootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"openapi", "--version", "2.0"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected openapi to fail for unsupported version 2.0, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported openapi version") {
		t.Fatalf("expected unsupported version error, got: %v", err)
	}
}
