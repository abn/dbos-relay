package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"database/sql"

	_ "modernc.org/sqlite"
)

func healthServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

func writeManifest(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "relay.yaml")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func TestDoctor_AllChecksPassSQLite(t *testing.T) {
	controlURL := controlPlaneURL(t)
	provisionApplication(t, controlURL, "orders")
	appURL := applicationSystemURL(t)
	createSchedule(t, appURL, "orders", "nightly-report")

	manifest := writeManifest(t, `
version: "1"
organisation: "default"
applications:
  - name: "orders"
data_plane:
  orders:
    connection_url: "`+appURL+`"
    integration: "direct"
    mode: "read-write"
    statement_timeout_secs: 5
    max_connections: 2
`)
	server := healthServer(t)

	report, err := Run(context.Background(), Options{
		DatabaseURL:  controlURL,
		ManifestPath: manifest,
		RelayURL:     server.URL,
		Env: env(map[string]string{
			"RELAY_INTERNAL_SECRET": "test-secret",
			"DBOS_APP_NAME":         "orders",
			"DBOS__VMID":            "orders-0",
		}),
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failed() != 0 {
		t.Fatalf("expected no failures, got report:\n%s", report.String())
	}

	expected := map[string]Status{
		"control-plane.connectivity":      StatusPass,
		"control-plane.read":              StatusPass,
		"control-plane.migrations":        StatusPass,
		"control-plane.write":             StatusPass,
		"rollback.control-plane":          StatusPass,
		"rollback.snapshot":               StatusSkip,
		"manifest.load":                   StatusPass,
		"data-plane.registration[orders]": StatusPass,
		"data-plane.manifest[orders]":     StatusPass,
		"data-plane.connectivity[orders]": StatusPass,
		"direct.schema[orders]":           StatusPass,
		"data-plane.read[orders]":         StatusPass,
		"data-plane.write[orders]":        StatusSkip,
		"schedules[orders]":               StatusPass,
		"rollback.application[orders]":    StatusSkip,
		"probe.secret":                    StatusPass,
		"probe.endpoint":                  StatusPass,
		"probe.identity":                  StatusPass,
	}
	for id, want := range expected {
		if got := findCheck(t, report, id); got.Status != want {
			t.Errorf("%s status = %s, want %s (detail: %s)", id, got.Status, want, got.Detail)
		}
	}

	if got := findCheck(t, report, "direct.schema[orders]"); !strings.Contains(got.Detail, "123") {
		t.Errorf("schema detail = %q, want schema 123", got.Detail)
	}
}

func TestDoctor_ControlPlaneConnectionFailure(t *testing.T) {
	server := healthServer(t)
	report, err := Run(context.Background(), Options{
		DatabaseURL: "sqlite://" + filepath.Join(t.TempDir(), "missing", "relay.db"),
		RelayURL:    server.URL,
		Env:         env(nil),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failed() != 1 {
		t.Fatalf("expected one failure, got report:\n%s", report.String())
	}
	if got := findCheck(t, report, "control-plane.connectivity"); got.Status != StatusFail {
		t.Errorf("connectivity status = %s, want FAIL", got.Status)
	}
	for _, id := range []string{"control-plane.read", "control-plane.migrations", "control-plane.write", "rollback.control-plane"} {
		if got := findCheck(t, report, id); got.Status != StatusSkip {
			t.Errorf("%s status = %s, want SKIP", id, got.Status)
		}
	}
}

func TestDoctor_MigrationBehindWarns(t *testing.T) {
	controlURL := controlPlaneURL(t)

	s, err := storeOpen(controlURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.MigrateDown(context.Background(), 1); err != nil {
		s.Close()
		t.Fatalf("migrate down: %v", err)
	}
	s.Close()

	server := healthServer(t)
	report, err := Run(context.Background(), Options{
		DatabaseURL: controlURL,
		RelayURL:    server.URL,
		Env:         env(nil),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failed() != 0 {
		t.Fatalf("expected no failures, got report:\n%s", report.String())
	}
	migrations := findCheck(t, report, "control-plane.migrations")
	if migrations.Status != StatusWarn || !strings.Contains(migrations.Detail, "behind") {
		t.Errorf("migrations = %+v, want WARN behind", migrations)
	}
	if rollback := findCheck(t, report, "rollback.control-plane"); rollback.Status != StatusPass {
		t.Errorf("rollback status = %s, want PASS", rollback.Status)
	}
}

func TestDoctor_DirtyMigrationFails(t *testing.T) {
	controlURL := controlPlaneURL(t)

	dbPath := strings.TrimPrefix(controlURL, "sqlite://")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := db.Exec("UPDATE schema_migrations SET dirty = 1"); err != nil {
		_ = db.Close()
		t.Fatalf("set dirty flag: %v", err)
	}
	_ = db.Close()

	server := healthServer(t)
	report, err := Run(context.Background(), Options{
		DatabaseURL: controlURL,
		RelayURL:    server.URL,
		Env:         env(nil),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failed() < 2 {
		t.Fatalf("expected migration and rollback failures, got report:\n%s", report.String())
	}
	if got := findCheck(t, report, "control-plane.migrations"); got.Status != StatusFail {
		t.Errorf("migrations status = %s, want FAIL", got.Status)
	}
	if got := findCheck(t, report, "rollback.control-plane"); got.Status != StatusFail {
		t.Errorf("rollback status = %s, want FAIL", got.Status)
	}
}

func TestDoctor_ManifestMissingApplicationFails(t *testing.T) {
	controlURL := controlPlaneURL(t)
	appURL := applicationSystemURL(t)
	manifest := writeManifest(t, `
version: "1"
applications:
  - name: "orders"
data_plane:
  ghost:
    connection_url: "`+appURL+`"
    integration: "hybrid"
`)
	server := healthServer(t)

	report, err := Run(context.Background(), Options{
		DatabaseURL:  controlURL,
		ManifestPath: manifest,
		RelayURL:     server.URL,
		Env:          env(nil),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	check := findCheck(t, report, "data-plane.registration[ghost]")
	if check.Status != StatusFail {
		t.Fatalf("registration check = %+v, want FAIL", check)
	}
}

func TestDoctor_ManifestOnlyApplicationWarns(t *testing.T) {
	controlURL := controlPlaneURL(t)
	appURL := applicationSystemURL(t)
	manifest := writeManifest(t, `
version: "1"
applications:
  - name: "pending"
data_plane:
  pending:
    connection_url: "`+appURL+`"
    integration: "hybrid"
`)
	server := healthServer(t)

	report, err := Run(context.Background(), Options{
		DatabaseURL:  controlURL,
		ManifestPath: manifest,
		RelayURL:     server.URL,
		Env:          env(nil),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	check := findCheck(t, report, "data-plane.registration[pending]")
	if check.Status != StatusWarn || !strings.Contains(check.Detail, "not yet in the control plane") {
		t.Fatalf("registration check = %+v, want WARN about provisioning", check)
	}
	if report.Failed() != 0 {
		t.Fatalf("expected no failures, got report:\n%s", report.String())
	}
}

func TestDoctor_ReportsNoSecrets(t *testing.T) {
	const password = "super-secret-password"
	controlURL := controlPlaneURL(t)
	manifest := writeManifest(t, `
version: "1"
applications:
  - name: "orders"
data_plane:
  orders:
    connection_url: "postgres://relay:`+password+`@127.0.0.1:1/orders?sslmode=disable"
    integration: "hybrid"
    mode: "read"
    statement_timeout_secs: 1
`)
	server := healthServer(t)

	report, err := Run(context.Background(), Options{
		DatabaseURL:  controlURL,
		ManifestPath: manifest,
		RelayURL:     server.URL,
		Env:          env(nil),
		Timeout:      2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(report.String(), password) {
		t.Fatalf("report leaked the database password:\n%s", report.String())
	}
}
