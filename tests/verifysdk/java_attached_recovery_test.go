package verifysdk_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// The Java Attached recovery cell is pinned to the SDK release and schema
// generation recorded in docs/usage/supported-sdks.md. A newer release or
// schema needs a fresh recovery run before the support claim moves.
const (
	javaAttachedSDKVersion    = "1.2.0"
	javaAttachedSchemaVersion = 114
)

// TestVerifySDK_JavaAttachedRecovery runs the Attached recovery cell for the
// Java SDK alone. It is separate from the full matrix so the Java boundary can
// be re-verified without starting every sample application.
func TestVerifySDK_JavaAttachedRecovery(t *testing.T) {
	if os.Getenv("RELAY_VERIFY_JAVA_RECOVERY") != "1" {
		t.Skip("skipping Java Attached recovery cell: set RELAY_VERIFY_JAVA_RECOVERY=1 (or run make verify-sdk/java-recovery)")
	}

	info := containerInfo{
		Name:          "deploy-app-java-1",
		SecondaryName: "deploy-app-java-secondary-1",
		AppName:       "java-sample-app",
		DBName:        "relay_java",
		Language:      "java",
		TriggerPort:   8083,
		SecondaryPort: 8087,
	}

	psOutput := runCmd(t, "podman", "ps", "--format", "{{.Names}}\t{{.Status}}")
	requiredContainers := []string{"deploy-relay-1", "deploy-postgres-1", info.Name, info.SecondaryName}
	var missing []string
	for _, name := range requiredContainers {
		if !strings.Contains(psOutput, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Skipf("skipping: missing required containers: %s", strings.Join(missing, ", "))
	}

	pin := javaExamplePin(t)
	if pin != javaAttachedSDKVersion {
		t.Fatalf("Java sample is pinned to %s, but this cell proves Attached recovery for %s; re-run recovery before moving the pin", pin, javaAttachedSDKVersion)
	}
	if !strings.Contains(runCmd(t, "podman", "logs", info.Name), "Launching DBOS v"+javaAttachedSDKVersion) {
		t.Fatalf("container %s did not report DBOS v%s at launch", info.Name, javaAttachedSDKVersion)
	}
	info.SDKVersion = javaAttachedSDKVersion

	if !waitForSecondaryExecutor(t, info, 60*time.Second) {
		t.Logf("[Java] Secondary executor did not register healthy, restarting %s", info.SecondaryName)
		_ = runCmd(t, "podman", "restart", info.SecondaryName)
		if !waitForSecondaryExecutor(t, info, 90*time.Second) {
			secLogs := runCmd(t, "podman", "logs", info.SecondaryName)
			t.Logf("[Java] Secondary logs on failed recovery registration:\n%s", secLogs)
			t.Fatalf("[Java] Timed out waiting for both executors of %s to register healthy", info.AppName)
		}
	}

	dbConn := getAppDBConn(t, info.DBName)
	defer func() { _ = dbConn.Close(context.Background()) }()

	var schemaVersion int
	if err := dbConn.QueryRow(context.Background(), `
		SELECT COALESCE(MAX(version), 0) FROM dbos.dbos_migrations;
	`).Scan(&schemaVersion); err != nil {
		t.Fatalf("[Java] Failed to read the SDK schema version: %v", err)
	}
	if schemaVersion != javaAttachedSchemaVersion {
		t.Fatalf("[Java] System database is at schema %d, but this cell proves Attached recovery at schema %d", schemaVersion, javaAttachedSchemaVersion)
	}
	t.Logf("[Java] Attached mode at DBOS v%s, system database schema %d", info.SDKVersion, schemaVersion)

	info = runChaosRecovery(t, "Java", info)

	if info.DeadKillDelta < 10*time.Second {
		t.Fatalf("[Java] DEAD transition occurred too quickly: %v < configured grace 10s", info.DeadKillDelta)
	}
	if info.TerminalOutcomes != 1 {
		t.Fatalf("[Java] Expected exactly 1 terminal outcome, got %d", info.TerminalOutcomes)
	}
	if info.StepReexecutions != 0 {
		t.Fatalf("[Java] Expected no step re-executions, got %d", info.StepReexecutions)
	}

	recoveryLog := strings.ToLower(info.SurvivorRecoveryLog)
	if strings.Contains(recoveryLog, "no explicit") || !strings.Contains(recoveryLog, "recover") {
		t.Fatalf("[Java] Survivor did not log a recovery request: %s", info.SurvivorRecoveryLog)
	}

	t.Logf("[Java] Attached recovery PASS: killed executor %s marked DEAD after %v, survivor %s recovered the workflow, terminal outcomes %d, step re-executions %d, recovery log %q",
		info.ExecutorID, info.DeadKillDelta.Round(time.Millisecond), info.SecondaryName, info.TerminalOutcomes, info.StepReexecutions, info.SurvivorRecoveryLog)
}
