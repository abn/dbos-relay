package doctor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dbos-inc/dbos-transact-golang/dbos"

	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
)

func controlPlaneURL(t *testing.T) string {
	t.Helper()
	url := "sqlite://" + filepath.Join(t.TempDir(), "relay.db")
	s, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open control-plane store: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		s.Close()
		t.Fatalf("migrate control-plane store: %v", err)
	}
	s.Close()
	return url
}

func storeOpen(url string) (*store.Store, error) {
	return store.Open(context.Background(), url)
}

func provisionApplication(t *testing.T, controlURL, appName string) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, controlURL)
	if err != nil {
		t.Fatalf("open control-plane store: %v", err)
	}
	defer s.Close()

	org, err := s.Queries().CreateOrganisation(ctx, "default")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}
	if _, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           appName,
		Settings:       []byte("{}"),
	}); err != nil {
		t.Fatalf("create application: %v", err)
	}
}

func applicationSystemURL(t *testing.T) string {
	t.Helper()
	url := "sqlite:" + filepath.Join(t.TempDir(), "app.db")
	if err := dbos.Migrate(context.Background(), url); err != nil {
		t.Fatalf("migrate application system database: %v", err)
	}
	return url
}

func createSchedule(t *testing.T, appURL, appName, scheduleName string) {
	t.Helper()
	ctx := context.Background()
	client, err := dbos.NewClient(ctx, dbos.ClientConfig{
		DatabaseURL: appURL,
		AppName:     appName,
	})
	if err != nil {
		t.Fatalf("create SDK client: %v", err)
	}
	defer func() {
		_ = client.Shutdown(client, 5*time.Second)
	}()
	if err := client.CreateSchedule(client, dbos.ScheduleSpec{
		ScheduleName: scheduleName,
		Schedule:     "0 0 0 * * *",
		WorkflowName: "nightly-report",
	}); err != nil {
		t.Fatalf("create schedule: %v", err)
	}
}

func findCheck(t *testing.T, report *Report, id string) Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("check %q not found in report: %+v", id, report.Checks)
	return Check{}
}

func env(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
