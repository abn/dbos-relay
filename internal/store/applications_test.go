package store

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/abn/relay/internal/store/gen"
)

func TestApplications(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	org, err := s.Queries().CreateOrganisation(ctx, "acme_apps")
	if err != nil {
		t.Fatalf("failed to create organisation: %v", err)
	}

	t.Run("CreateApplication and GetApplicationByName", func(t *testing.T) {
		settings := []byte(`{"timeout_ms":5000}`)
		created, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
			OrganisationID: org.ID,
			Name:           "service-alpha",
			Settings:       settings,
		})
		if err != nil {
			t.Fatalf("CreateApplication failed: %v", err)
		}
		if !created.ID.Valid {
			t.Fatal("expected valid application ID")
		}
		if created.OrganisationID != org.ID {
			t.Errorf("expected organisation ID %v, got %v", org.ID, created.OrganisationID)
		}
		if created.Name != "service-alpha" {
			t.Errorf("expected name 'service-alpha', got %q", created.Name)
		}
		if !bytes.Equal(created.Settings, settings) {
			t.Errorf("expected settings %s, got %s", settings, created.Settings)
		}
		if !created.CreatedAt.Valid {
			t.Fatal("expected valid created_at")
		}

		fetched, err := s.Queries().GetApplicationByName(ctx, gen.GetApplicationByNameParams{
			OrganisationID: org.ID,
			Name:           "service-alpha",
		})
		if err != nil {
			t.Fatalf("GetApplicationByName failed: %v", err)
		}
		if fetched.ID != created.ID {
			t.Errorf("expected ID %v, got %v", created.ID, fetched.ID)
		}
		if fetched.Name != created.Name {
			t.Errorf("expected name %q, got %q", created.Name, fetched.Name)
		}
		if !bytes.Equal(fetched.Settings, settings) {
			t.Errorf("expected settings %s, got %s", settings, fetched.Settings)
		}

		// Non-existent application
		_, err = s.Queries().GetApplicationByName(ctx, gen.GetApplicationByNameParams{
			OrganisationID: org.ID,
			Name:           "non-existent",
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("expected pgx.ErrNoRows, got %v", err)
		}
	})

	t.Run("ListApplicationsByOrganisation", func(t *testing.T) {
		listOrg, err := s.Queries().CreateOrganisation(ctx, "acme_list")
		if err != nil {
			t.Fatalf("failed to create organisation: %v", err)
		}

		otherOrg, err := s.Queries().CreateOrganisation(ctx, "other_org")
		if err != nil {
			t.Fatalf("failed to create other organisation: %v", err)
		}

		emptyList, err := s.Queries().ListApplicationsByOrganisation(ctx, listOrg.ID)
		if err != nil {
			t.Fatalf("ListApplicationsByOrganisation failed on empty list: %v", err)
		}
		if len(emptyList) != 0 {
			t.Fatalf("expected 0 applications, got %d", len(emptyList))
		}

		app1, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
			OrganisationID: listOrg.ID,
			Name:           "app-first",
			Settings:       []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("failed to create app1: %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		app2, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
			OrganisationID: listOrg.ID,
			Name:           "app-second",
			Settings:       []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("failed to create app2: %v", err)
		}

		// App in different org should not appear
		_, err = s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
			OrganisationID: otherOrg.ID,
			Name:           "other-app",
			Settings:       []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("failed to create other app: %v", err)
		}

		apps, err := s.Queries().ListApplicationsByOrganisation(ctx, listOrg.ID)
		if err != nil {
			t.Fatalf("ListApplicationsByOrganisation failed: %v", err)
		}
		if len(apps) != 2 {
			t.Fatalf("expected 2 applications, got %d", len(apps))
		}
		if apps[0].ID != app2.ID {
			t.Errorf("expected first listed app to be %v (most recent), got %v", app2.ID, apps[0].ID)
		}
		if apps[1].ID != app1.ID {
			t.Errorf("expected second listed app to be %v, got %v", app1.ID, apps[1].ID)
		}
	})

	t.Run("UpsertApplication", func(t *testing.T) {
		upsertOrg, err := s.Queries().CreateOrganisation(ctx, "acme_upsert")
		if err != nil {
			t.Fatalf("failed to create organisation: %v", err)
		}

		initialSettings := []byte(`{"env":"prod"}`)
		app, err := s.Queries().UpsertApplication(ctx, gen.UpsertApplicationParams{
			OrganisationID: upsertOrg.ID,
			Name:           "upsert-app",
			Settings:       initialSettings,
		})
		if err != nil {
			t.Fatalf("UpsertApplication (insert) failed: %v", err)
		}
		if app.Name != "upsert-app" {
			t.Errorf("expected name 'upsert-app', got %q", app.Name)
		}
		if !bytes.Equal(app.Settings, initialSettings) {
			t.Errorf("expected settings %s, got %s", initialSettings, app.Settings)
		}

		// Upsert again to update settings
		updatedSettings := []byte(`{"env":"staging","debug":true}`)
		appUpdated, err := s.Queries().UpsertApplication(ctx, gen.UpsertApplicationParams{
			OrganisationID: upsertOrg.ID,
			Name:           "upsert-app",
			Settings:       updatedSettings,
		})
		if err != nil {
			t.Fatalf("UpsertApplication (conflict update) failed: %v", err)
		}
		if appUpdated.ID != app.ID {
			t.Errorf("expected same application ID %v, got %v", app.ID, appUpdated.ID)
		}
		if !bytes.Equal(appUpdated.Settings, updatedSettings) {
			t.Errorf("expected updated settings %s, got %s", updatedSettings, appUpdated.Settings)
		}

		fetched, err := s.Queries().GetApplicationByName(ctx, gen.GetApplicationByNameParams{
			OrganisationID: upsertOrg.ID,
			Name:           "upsert-app",
		})
		if err != nil {
			t.Fatalf("GetApplicationByName failed: %v", err)
		}
		if !bytes.Equal(fetched.Settings, updatedSettings) {
			t.Errorf("expected persisted settings %s, got %s", updatedSettings, fetched.Settings)
		}
	})

	t.Run("UpdateApplicationSettings", func(t *testing.T) {
		app, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
			OrganisationID: org.ID,
			Name:           "service-beta",
			Settings:       []byte(`{"version":1}`),
		})
		if err != nil {
			t.Fatalf("CreateApplication failed: %v", err)
		}

		newSettings := []byte(`{"version":2}`)
		updated, err := s.Queries().UpdateApplicationSettings(ctx, gen.UpdateApplicationSettingsParams{
			OrganisationID: org.ID,
			Name:           app.Name,
			Settings:       newSettings,
		})
		if err != nil {
			t.Fatalf("UpdateApplicationSettings failed: %v", err)
		}
		if updated.ID != app.ID {
			t.Errorf("expected ID %v, got %v", app.ID, updated.ID)
		}
		if !bytes.Equal(updated.Settings, newSettings) {
			t.Errorf("expected settings %s, got %s", newSettings, updated.Settings)
		}

		fetched, err := s.Queries().GetApplicationByName(ctx, gen.GetApplicationByNameParams{
			OrganisationID: org.ID,
			Name:           app.Name,
		})
		if err != nil {
			t.Fatalf("GetApplicationByName failed: %v", err)
		}
		if !bytes.Equal(fetched.Settings, newSettings) {
			t.Errorf("expected settings %s, got %s", newSettings, fetched.Settings)
		}

		// Update non-existent application
		_, err = s.Queries().UpdateApplicationSettings(ctx, gen.UpdateApplicationSettingsParams{
			OrganisationID: org.ID,
			Name:           "missing-app",
			Settings:       newSettings,
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("expected pgx.ErrNoRows, got %v", err)
		}
	})

	t.Run("DeleteApplication", func(t *testing.T) {
		app, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
			OrganisationID: org.ID,
			Name:           "service-gamma",
			Settings:       []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("CreateApplication failed: %v", err)
		}

		deleted, err := s.Queries().DeleteApplication(ctx, gen.DeleteApplicationParams{
			OrganisationID: org.ID,
			Name:           app.Name,
		})
		if err != nil {
			t.Fatalf("DeleteApplication failed: %v", err)
		}
		if deleted.ID != app.ID {
			t.Errorf("expected deleted ID %v, got %v", app.ID, deleted.ID)
		}

		_, err = s.Queries().GetApplicationByName(ctx, gen.GetApplicationByNameParams{
			OrganisationID: org.ID,
			Name:           app.Name,
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("expected pgx.ErrNoRows after delete, got %v", err)
		}

		// Delete non-existent application
		_, err = s.Queries().DeleteApplication(ctx, gen.DeleteApplicationParams{
			OrganisationID: org.ID,
			Name:           app.Name,
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("expected pgx.ErrNoRows when deleting non-existent application, got %v", err)
		}
	})

	t.Run("UpsertOrganisation", func(t *testing.T) {
		org1, err := s.Queries().UpsertOrganisation(ctx, "acme_upsert_org")
		if err != nil {
			t.Fatalf("UpsertOrganisation (insert) failed: %v", err)
		}
		if !org1.ID.Valid {
			t.Fatal("expected valid organisation ID")
		}
		if org1.Name != "acme_upsert_org" {
			t.Errorf("expected name 'acme_upsert_org', got %q", org1.Name)
		}

		// Upsert again with the same name
		org2, err := s.Queries().UpsertOrganisation(ctx, "acme_upsert_org")
		if err != nil {
			t.Fatalf("UpsertOrganisation (conflict update) failed: %v", err)
		}
		if org2.ID != org1.ID {
			t.Errorf("expected same organisation ID %v, got %v", org1.ID, org2.ID)
		}
		if org2.Name != org1.Name {
			t.Errorf("expected name %q, got %q", org1.Name, org2.Name)
		}

		fetched, err := s.Queries().GetOrganisationByName(ctx, "acme_upsert_org")
		if err != nil {
			t.Fatalf("GetOrganisationByName failed: %v", err)
		}
		if fetched.ID != org1.ID {
			t.Errorf("expected fetched ID %v, got %v", org1.ID, fetched.ID)
		}
	})
}
