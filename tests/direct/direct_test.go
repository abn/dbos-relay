package direct_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/protocol"
)

type fixtureOutput struct {
	CompletedWorkflowID string `json:"completed_workflow_id"`
	BlockingWorkflowID  string `json:"blocking_workflow_id"`
	ExecutorID          string `json:"executor_id"`
}

func TestDirectPythonSQLite(t *testing.T) {
	if !commandSucceeds("python3", "-c", "import dbos") {
		t.Skip("python dbos package is not installed")
	}
	path := filepath.Join(t.TempDir(), "sys.db")
	runCase(t, directCase{
		name:          "python-sqlite",
		appName:       "direct-python-sqlite",
		executorID:    "python-sqlite-fixture",
		command:       []string{"python3", "main.py"},
		dir:           filepath.Join("fixtures", "python"),
		expectedValue: "hello python",
	}, "sqlite:///"+path, "sqlite:"+path)
}

func TestDirectCrossLanguage(t *testing.T) {
	baseURL := os.Getenv("RELAY_TEST_DATABASE_URL")
	if baseURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}
	if !strings.HasPrefix(baseURL, "postgres://") && !strings.HasPrefix(baseURL, "postgresql://") {
		t.Skip("Direct cross-language test requires PostgreSQL")
	}

	t.Run("Python", func(t *testing.T) {
		runDirectCase(t, baseURL, directCase{
			name:          "python",
			database:      "relay_direct_python",
			appName:       "direct-python",
			executorID:    "python-direct-fixture",
			command:       []string{"python3", "main.py"},
			dir:           filepath.Join("fixtures", "python"),
			expectedValue: "hello python",
			available: func() bool {
				return commandSucceeds("python3", "-c", "import dbos")
			},
			skipReason: "python dbos package is not installed",
		})
	})

	t.Run("TypeScript", func(t *testing.T) {
		runDirectCase(t, baseURL, directCase{
			name:          "typescript",
			database:      "relay_direct_typescript",
			appName:       "direct-typescript",
			executorID:    "typescript-direct-fixture",
			command:       []string{"node", "dist/main.js"},
			dir:           filepath.Join("fixtures", "typescript"),
			expectedValue: "hello typescript",
			available: func() bool {
				if _, err := os.Stat(filepath.Join("fixtures", "typescript", "dist", "main.js")); err != nil {
					return false
				}
				return commandSucceedsIn("node", filepath.Join("fixtures", "typescript"), "-e", "require('@dbos-inc/dbos-sdk')")
			},
			skipReason: "TypeScript fixture is not built or @dbos-inc/dbos-sdk is not installed",
		})
	})
}

type directCase struct {
	name          string
	database      string
	appName       string
	executorID    string
	command       []string
	dir           string
	expectedValue string
	available     func() bool
	skipReason    string
}

func runDirectCase(t *testing.T, baseURL string, tc directCase) {
	t.Helper()
	if !tc.available() {
		t.Skip(tc.skipReason)
	}
	dbURL := createDatabase(t, baseURL, tc.database)
	runCase(t, tc, dbURL, dbURL)
}

func runCase(t *testing.T, tc directCase, fixtureDBURL, clientDBURL string) {
	t.Helper()
	output := runFixture(t, fixtureDBURL, tc)
	if output.CompletedWorkflowID == "" || output.BlockingWorkflowID == "" {
		t.Fatalf("fixture did not return workflow IDs: %+v", output)
	}

	client, err := dataplane.NewSDKClient(dataplane.AppConfig{
		ApplicationName:  tc.appName,
		DatabaseURL:      clientDBURL,
		Mode:             dataplane.ModeReadWrite,
		Integration:      dataplane.IntegrationDirect,
		MaxConnections:   2,
		StatementTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("creating Direct client: %v", err)
	}
	defer func() { _ = client.Close() }()

	if output.ExecutorID != tc.executorID {
		t.Fatalf("fixture executor ID = %q, want %q", output.ExecutorID, tc.executorID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	assertWorkflowStatus(t, ctx, client, output.CompletedWorkflowID, "SUCCESS")
	assertWorkflowOutput(t, ctx, client, output.CompletedWorkflowID, tc.expectedValue)
	assertSteps(t, ctx, client, output.CompletedWorkflowID)
	assertSteps(t, ctx, client, output.BlockingWorkflowID)

	ids, err := client.Recover(ctx, []string{output.ExecutorID}, "")
	if err != nil {
		t.Fatalf("recovering pending workflow: %v", err)
	}
	if !contains(ids, output.BlockingWorkflowID) {
		t.Fatalf("recovery did not return the blocking workflow %s: %v", output.BlockingWorkflowID, ids)
	}
	pending, err := client.HasPendingWorkflows(ctx, output.ExecutorID, "")
	if err != nil {
		t.Fatalf("checking pending workflows: %v", err)
	}
	if pending {
		t.Fatal("recovery left a PENDING workflow for the fixture executor")
	}

	if _, err := client.Dispatch(ctx, &protocol.CancelWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeCancel,
			RequestID: uuid.NewString(),
		},
		WorkflowID: output.BlockingWorkflowID,
	}); err != nil {
		t.Fatalf("cancelling the blocking workflow: %v", err)
	}
	assertWorkflowStatus(t, ctx, client, output.BlockingWorkflowID, "CANCELLED")
	assertSteps(t, ctx, client, output.BlockingWorkflowID)
}

func createDatabase(t *testing.T, baseURL, name string) string {
	t.Helper()
	u, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parsing database URL: %v", err)
	}
	adminURL := u.String()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connecting to admin database: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	quoted := pgx.Identifier{name}.Sanitize()
	_, _ = conn.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, name)
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+quoted); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "must be owner") {
			t.Skipf("test database %s is owned by another role: %v", name, err)
		}
		t.Fatalf("dropping database %s: %v", name, err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "permission denied") || strings.Contains(strings.ToLower(err.Error()), "must be superuser") {
			t.Skipf("test database role cannot create databases: %v", err)
		}
		t.Fatalf("creating database %s: %v", name, err)
	}
	u.Path = "/" + name
	return u.String()
}

func runFixture(t *testing.T, dbURL string, tc directCase) fixtureOutput {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tc.command[0], tc.command[1:]...)
	cmd.Dir = tc.dir
	cmd.Env = append(os.Environ(),
		"DBOS_APP_NAME="+tc.appName,
		"DBOS_SYSTEM_DATABASE_URL="+dbURL,
		"DBOS__VMID="+tc.executorID,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s fixture failed: %v\nstdout:\n%s\nstderr:\n%s", tc.name, err, stdout.String(), stderr.String())
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var out fixtureOutput
		if err := json.Unmarshal([]byte(line), &out); err == nil && out.CompletedWorkflowID != "" {
			return out
		}
	}
	t.Fatalf("%s fixture did not emit a workflow result\nstdout:\n%s\nstderr:\n%s", tc.name, stdout.String(), stderr.String())
	return fixtureOutput{}
}

func assertWorkflowStatus(t *testing.T, ctx context.Context, client dataplane.Client, workflowID, want string) {
	t.Helper()
	resp, err := client.Dispatch(ctx, &protocol.GetWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflow,
			RequestID: uuid.NewString(),
		},
		WorkflowID: workflowID,
	})
	if err != nil {
		t.Fatalf("getting workflow %s: %v", workflowID, err)
	}
	get, ok := resp.(*protocol.GetWorkflowResponse)
	if !ok || get.Output == nil || get.Output.Status == nil {
		t.Fatalf("unexpected workflow response for %s: %#v", workflowID, resp)
	}
	if *get.Output.Status != want {
		t.Fatalf("workflow %s status = %s, want %s", workflowID, *get.Output.Status, want)
	}
}

func assertWorkflowOutput(t *testing.T, ctx context.Context, client dataplane.Client, workflowID, want string) {
	t.Helper()
	resp, err := client.Dispatch(ctx, &protocol.GetWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflow,
			RequestID: uuid.NewString(),
		},
		WorkflowID: workflowID,
	})
	if err != nil {
		t.Fatalf("getting workflow %s: %v", workflowID, err)
	}
	get, ok := resp.(*protocol.GetWorkflowResponse)
	if !ok || get.Output == nil || get.Output.Output == nil {
		t.Fatalf("workflow %s has no output: %#v", workflowID, resp)
	}
	var got string
	if err := json.Unmarshal([]byte(*get.Output.Output), &got); err != nil {
		t.Fatalf("decoding workflow output %q: %v", *get.Output.Output, err)
	}
	if got != want {
		t.Fatalf("workflow %s output = %q, want %q", workflowID, got, want)
	}
}

func assertSteps(t *testing.T, ctx context.Context, client dataplane.Client, workflowID string) {
	t.Helper()
	resp, err := client.Dispatch(ctx, &protocol.ListStepsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListSteps,
			RequestID: uuid.NewString(),
		},
		WorkflowID: workflowID,
	})
	if err != nil {
		t.Fatalf("listing steps for %s: %v", workflowID, err)
	}
	steps, ok := resp.(*protocol.ListStepsResponse)
	if !ok || steps.Output == nil || len(*steps.Output) == 0 {
		t.Fatalf("workflow %s has no steps: %#v", workflowID, resp)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func commandSucceeds(name string, args ...string) bool {
	return commandSucceedsIn(name, "", args...)
}

func commandSucceedsIn(name, dir string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run() == nil
}
