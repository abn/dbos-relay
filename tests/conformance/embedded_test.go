package conformance_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/conformance"
	"github.com/abn/relay/internal/dashboard"
	"github.com/abn/relay/internal/hub"
	"github.com/abn/relay/internal/metrics"
	"github.com/abn/relay/internal/router"
	"github.com/abn/relay/internal/store"
	storegen "github.com/abn/relay/internal/store/gen"
)

func TestConformance_EmbeddedSQLiteSuite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "conformance_embedded.db")
	dbURL := "sqlite://" + dbPath

	s, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to open embedded sqlite store: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate embedded sqlite store: %v", err)
	}

	orgName := "embedded-acme"
	appName := "embedded-conformance-app"

	org, err := s.Queries().CreateOrganisation(ctx, orgName)
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	_, err = s.Queries().CreateApplication(ctx, storegen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           appName,
		Settings:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	plainKey, rec, err := auth.Mint()
	if err != nil {
		t.Fatalf("failed to mint key: %v", err)
	}

	_, err = s.Queries().CreateAPIKey(ctx, storegen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             "embedded-conformance-key",
		Lookup:           rec.Lookup,
		KeyHash:          rec.Hash,
		ApplicationNames: []string{appName},
		Permissions:      []string{"application.read", "application.write", "websocket.connect"},
	})
	if err != nil {
		t.Fatalf("failed to save api key: %v", err)
	}

	cfg := &config.Config{
		DatabaseURL:      dbURL,
		Embedded:         true,
		ListenAddr:       ":0",
		ExecutorDeadline: 5 * time.Second,
	}
	logger := slog.Default()

	h := hub.New(s, cfg, logger)
	defer func() { _ = h.Close() }()

	r := router.New(s.Queries(), h)
	apiServer := api.NewServer(r, s.Queries(), logger)
	handler := api.NewHandler(s, apiServer)
	dashHandler := dashboard.Handler(handler)

	mux := http.NewServeMux()
	mux.Handle("/", dashHandler)
	mux.Handle("/websocket/", h)
	metricsH := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authHeader := req.Header.Get("Authorization")
		if authHeader == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.EqualFold(authHeader, "Bearer "+plainKey) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		reqWithAuth := req.WithContext(auth.WithIdentity(req.Context(), &auth.UserIdentity{
			IsAdmin:     true,
			Permissions: []string{"application.read", "application.write", "websocket.connect"},
		}))
		metrics.NewHandler(s.Queries()).ServeHTTP(w, reqWithAuth)
	})
	mux.Handle("/v1/metrics", metricsH)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	confCfg := conformance.Config{
		TargetURL:    ts.URL,
		ConductorKey: plainKey,
		OrgName:      orgName,
		AppName:      appName,
		Output:       os.Stdout,
		Timeout:      15 * time.Second,
	}

	report, err := conformance.Run(ctx, confCfg)
	if err != nil {
		t.Fatalf("conformance run against embedded relay failed: %v", err)
	}

	if !report.AllPassed {
		t.Errorf("embedded conformance suite failed: %d/%d batteries passed", report.TotalPass, len(report.Batteries))
		for _, b := range report.Batteries {
			if b.Status == conformance.StatusFail {
				t.Errorf("battery %d (%s) failed: %s", b.ID, b.Title, b.Error)
			}
		}
	}
}
