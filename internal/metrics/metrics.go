// Package metrics provides Prometheus and OpenMetrics scrape endpoints for Relay.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/store/gen"
)

// Store specifies the queries needed for metrics scraping.
type Store interface {
	ListAllApplications(ctx context.Context) ([]gen.Application, error)
	ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error)
	GetAPIKeyByLookup(ctx context.Context, lookup string) (gen.ApiKey, error)
}

// Handler serves the OpenMetrics / Prometheus scrape endpoint.
type Handler struct {
	store Store
}

// NewHandler creates a new metrics Handler.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			http.Error(w, "invalid authorization header", http.StatusUnauthorized)
			return
		}
		rawKey := parts[1]
		lookup := auth.Lookup(rawKey)
		keyRec, err := h.store.GetAPIKeyByLookup(r.Context(), lookup)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !auth.Verify(rawKey, keyRec.KeyHash) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	appsFilter := r.URL.Query()["applications"]
	metricsFilter := r.URL.Query()["metrics"]

	apps, err := h.store.ListAllApplications(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list applications: %v", err), http.StatusInternalServerError)
		return
	}

	var sb strings.Builder

	emitMetric := func(name, help string, entries map[string]int) {
		if len(metricsFilter) > 0 && !contains(metricsFilter, name) {
			return
		}
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
		sb.WriteString(fmt.Sprintf("# TYPE %s gauge\n", name))
		if len(entries) == 0 {
			return
		}

		keys := make([]string, 0, len(entries))
		for k := range entries {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			sb.WriteString(fmt.Sprintf("%s%s %d\n", name, k, entries[k]))
		}
	}

	executorCounts := make(map[string]int)

	for _, app := range apps {
		if len(appsFilter) > 0 && !contains(appsFilter, app.Name) {
			continue
		}

		execs, err := h.store.ListExecutorsByApplication(r.Context(), app.ID)
		if err != nil {
			continue
		}

		for _, exec := range execs {
			status := "HEALTHY"
			switch exec.Status {
			case "connected":
				status = "HEALTHY"
			case "disconnected":
				status = "DISCONNECTED"
			case "dead":
				status = "DEAD"
			}

			version := exec.ApplicationVersion
			if version == "" {
				version = "unknown"
			}

			labels := fmt.Sprintf(`{application="%s",application_version="%s",status="%s"}`, app.Name, version, status)
			executorCounts[labels]++
		}
	}

	emitMetric("dbos_conductor_v1_executor_count", "Number of registered executors.", executorCounts)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sb.String()))
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
