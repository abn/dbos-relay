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

// GroupedExecutorStore provides an optimized single-query grouped aggregation across applications.
type GroupedExecutorStore interface {
	GetExecutorCountsGrouped(ctx context.Context) ([]ExecutorCountRow, error)
}

// ExecutorCountRow represents aggregated executor counts grouped by application, version, and status.
type ExecutorCountRow struct {
	ApplicationName    string
	ApplicationVersion string
	Status             string
	Count              int64
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

	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	perms := identity.Permissions
	if len(perms) == 0 && identity.IsAPIKey {
		perms = auth.CatalogPermissions()
	}

	if !identity.IsAdmin && !auth.HasPermission(perms, auth.PermApplicationRead) {
		http.Error(w, "forbidden: missing application.read permission", http.StatusForbidden)
		return
	}

	appsFilter := r.URL.Query()["applications"]
	metricsFilter := r.URL.Query()["metrics"]
	_ = r.URL.Query()["workflow_names"]

	isOpenMetrics := strings.Contains(r.Header.Get("Accept"), "application/openmetrics-text")

	if len(metricsFilter) > 0 && !contains(metricsFilter, "dbos_conductor_v1_executor_count") {
		if isOpenMetrics {
			w.Header().Set("Content-Type", "application/openmetrics-text; version=1.0.0; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("# EOF\n"))
		} else {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			w.WriteHeader(http.StatusOK)
		}
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

	if groupedStore, ok := h.store.(GroupedExecutorStore); ok {
		rows, err := groupedStore.GetExecutorCountsGrouped(r.Context())
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to query grouped executor metrics: %v", err), http.StatusInternalServerError)
			return
		}
		for _, row := range rows {
			if len(appsFilter) > 0 && !contains(appsFilter, row.ApplicationName) {
				continue
			}
			if !identity.IsAdmin {
				if len(identity.ApplicationNames) > 0 && !contains(identity.ApplicationNames, row.ApplicationName) {
					continue
				}
			}
			status := "HEALTHY"
			switch row.Status {
			case "connected":
				status = "HEALTHY"
			case "disconnected":
				status = "DISCONNECTED"
			case "dead":
				status = "DEAD"
			}
			version := sanitizeLabel(row.ApplicationVersion)
			if version == "" {
				version = "unknown"
			}
			labels := fmt.Sprintf(`{application="%s",application_version="%s",status="%s"}`, sanitizeLabel(row.ApplicationName), version, status)
			executorCounts[labels] += int(row.Count)
		}
	} else {
		apps, err := h.store.ListAllApplications(r.Context())
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to list applications: %v", err), http.StatusInternalServerError)
			return
		}

		for _, app := range apps {
			if len(appsFilter) > 0 && !contains(appsFilter, app.Name) {
				continue
			}

			if !identity.IsAdmin {
				if app.OrganisationID != identity.OrgID {
					continue
				}
				if len(identity.ApplicationNames) > 0 && !contains(identity.ApplicationNames, app.Name) {
					continue
				}
			}

			execs, err := h.store.ListExecutorsByApplication(r.Context(), app.ID)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to list executors for application %s: %v", app.Name, err), http.StatusInternalServerError)
				return
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

				version := sanitizeLabel(exec.ApplicationVersion)
				if version == "" {
					version = "unknown"
				}

				labels := fmt.Sprintf(`{application="%s",application_version="%s",status="%s"}`, sanitizeLabel(app.Name), version, status)
				executorCounts[labels]++
			}
		}
	}

	emitMetric("dbos_conductor_v1_executor_count", "Number of registered executors.", executorCounts)

	if isOpenMetrics {
		sb.WriteString("# EOF\n")
		w.Header().Set("Content-Type", "application/openmetrics-text; version=1.0.0; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	}
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

func sanitizeLabel(s string) string {
	runes := []rune(s)
	if len(runes) > 64 {
		runes = runes[:64]
	}
	s = string(runes)
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
