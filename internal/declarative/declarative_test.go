package declarative_test

import (
	"context"
	"strings"
	"testing"

	"github.com/abn/relay/internal/declarative"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/testdb"
)

const sampleValidYAML = `version: "1"
organisation: "testorg"
applications:
  - name: "order-service"
    description: "Processes customer orders"
  - name: "payment-service"

alert_rules:
  - app: "order-service"
    rule_type: "UnresponsiveApplication"
    receiving_app: "payment-service"
    min_interval_secs: 60
    metadata:
      threshold: 1
  - app: "order-service"
    rule_type: "RecoveryFlapping"
    min_interval_secs: 300
    metadata:
      threshold: 3

data_plane:
  order-service:
    connection_url: "postgres://user:pass@localhost:5432/orders"
    mode: "read"
    statement_timeout_secs: 15
    max_connections: 5
`

func TestParse_Valid(t *testing.T) {
	cfg, err := declarative.Parse([]byte(sampleValidYAML))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if err := declarative.Validate(cfg); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if cfg.Version != "1" {
		t.Errorf("expected version 1, got %s", cfg.Version)
	}
	if cfg.Organisation != "testorg" {
		t.Errorf("expected org testorg, got %s", cfg.Organisation)
	}
	if len(cfg.Applications) != 2 {
		t.Errorf("expected 2 applications, got %d", len(cfg.Applications))
	}
	if len(cfg.AlertRules) != 2 {
		t.Errorf("expected 2 alert rules, got %d", len(cfg.AlertRules))
	}
	if len(cfg.DataPlanes) != 1 {
		t.Errorf("expected 1 data_plane, got %d", len(cfg.DataPlanes))
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "unsupported version",
			yaml: `version: "2"
applications:
  - name: "valid-name"`,
			wantErr: "unsupported configuration version",
		},
		{
			name: "invalid app name uppercase",
			yaml: `version: "1"
applications:
  - name: "Invalid_Name"`,
			wantErr: "invalid application name",
		},
		{
			name: "duplicate app name",
			yaml: `version: "1"
applications:
  - name: "app-one"
  - name: "app-one"`,
			wantErr: "duplicate application name",
		},
		{
			name: "unknown alert rule type",
			yaml: `version: "1"
applications:
  - name: "app-one"
alert_rules:
  - app: "app-one"
    rule_type: "UnknownRule"`,
			wantErr: "unknown alert rule type",
		},
		{
			name: "data plane missing connection url",
			yaml: `version: "1"
data_plane:
  app-one:
    mode: "read"`,
			wantErr: "requires connection_url or connection_string_from",
		},
		{
			name: "data plane invalid mode",
			yaml: `version: "1"
data_plane:
  app-one:
    connection_url: "postgres://localhost/db"
    mode: "super-admin"`,
			wantErr: "invalid data_plane mode",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := declarative.Parse([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			err = declarative.Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestDataPlane_ResolveConnectionURL(t *testing.T) {
	t.Setenv("TEST_ORDER_DB", "postgres://user:secret@localhost:5432/orders")

	// 1. Env variable in connection_string_from
	dp1 := declarative.DataPlane{
		ConnectionStringFrom: &declarative.ConnectionSource{Env: "TEST_ORDER_DB"},
	}
	url1, err := dp1.ResolveConnectionURL()
	if err != nil || url1 != "postgres://user:secret@localhost:5432/orders" {
		t.Fatalf("expected resolved env DSN, got %s (err: %v)", url1, err)
	}

	// 2. ${ENV} expansion in connection_url
	dp2 := declarative.DataPlane{
		ConnectionURL: "${TEST_ORDER_DB}",
	}
	url2, err := dp2.ResolveConnectionURL()
	if err != nil || url2 != "postgres://user:secret@localhost:5432/orders" {
		t.Fatalf("expected expanded env DSN, got %s (err: %v)", url2, err)
	}
}

func TestPlan_SummaryAndString(t *testing.T) {
	plan := &declarative.Plan{
		Items: []declarative.DiffItem{
			{Action: declarative.ActionCreate, Kind: "Application", Name: "app-1"},
			{Action: declarative.ActionUnchanged, Kind: "Application", Name: "app-2"},
			{Action: declarative.ActionDelete, Kind: "AlertRule", Name: "app-2/SlowQueue"},
		},
	}

	summary := plan.Summary()
	if !strings.Contains(summary, "1 to create") || !strings.Contains(summary, "1 to delete") {
		t.Errorf("unexpected summary: %s", summary)
	}

	str := plan.String()
	if !strings.Contains(str, "+ [CREATE] Application (app-1)") {
		t.Errorf("unexpected plan string: %s", str)
	}
	if !strings.Contains(str, "- [DELETE] AlertRule (app-2/SlowQueue)") {
		t.Errorf("unexpected plan string: %s", str)
	}
}

func TestDiffAndApply_LiveDB(t *testing.T) {
	url, err := testdb.URL("declarative")
	if err != nil {
		t.Fatalf("deriving test database url: %v", err)
	}
	if url == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("opening test store: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrating store: %v", err)
	}
	defer func() {
		_ = s.Truncate(context.Background())
	}()

	cfg, err := declarative.Parse([]byte(sampleValidYAML))
	if err != nil {
		t.Fatalf("parsing config: %v", err)
	}

	// First diff: everything should be created
	diffPlan, err := declarative.Diff(ctx, s, cfg)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if len(diffPlan.Items) == 0 {
		t.Fatal("expected diff items, got 0")
	}

	// Apply
	applyPlan, err := declarative.Apply(ctx, s, cfg)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(applyPlan.Items) == 0 {
		t.Fatal("expected apply items, got 0")
	}

	// Second diff: everything should now be unchanged
	secondDiff, err := declarative.Diff(ctx, s, cfg)
	if err != nil {
		t.Fatalf("Second diff failed: %v", err)
	}
	for _, item := range secondDiff.Items {
		if item.Kind == "Application" && item.Action != declarative.ActionUnchanged {
			t.Errorf("expected application to be UNCHANGED, got %v", item)
		}
	}
}
