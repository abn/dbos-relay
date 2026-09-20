// Package metrics provides Prometheus and OpenMetrics scrape endpoints for Relay.
//
// Sources: https://docs.dbos.dev/production/metrics (confirmed 2026-09-08)
// and the vendored Conductor OpenAPI spec in api/spec/openapi.json.
// Wire aggregation uses the executor protocol derived from the public
// DBOS Transact SDKs (see docs/protocol/executor-ws.md). Relay never
// queries application databases directly; workflow and step families
// are computed by dispatching aggregate requests to a healthy executor
// per application, with live executors taking precedence. When no
// executor answers, those series are omitted and executor counts from
// the control plane registry are still served.
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

// Store specifies the queries needed for metrics scraping.
type Store interface {
	ListAllApplications(ctx context.Context) ([]gen.Application, error)
	ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error)
	GetAPIKeyByLookup(ctx context.Context, lookup string) (gen.ApiKey, error)
}

// Dispatcher dispatches aggregate requests to a healthy executor.
// It matches the hub signature used by alerting evaluation.
type Dispatcher interface {
	Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

// GroupedExecutorStore provides an optimized single-query grouped aggregation across applications.
type GroupedExecutorStore interface {
	GetExecutorCountsGrouped(ctx context.Context) ([]ExecutorCountRow, error)
}

// OrgGroupedExecutorStore provides an optimized organization-scoped grouped aggregation.
type OrgGroupedExecutorStore interface {
	GetExecutorCountsGroupedByOrg(ctx context.Context, orgID pgtype.UUID) ([]ExecutorCountRow, error)
}

// GenGroupedStore provides sqlc-generated grouped aggregation across applications.
type GenGroupedStore interface {
	GetExecutorCountsGrouped(ctx context.Context) ([]gen.GetExecutorCountsGroupedRow, error)
}

// GenOrgGroupedStore provides sqlc-generated organization-scoped grouped aggregation.
type GenOrgGroupedStore interface {
	GetExecutorCountsGroupedByOrg(ctx context.Context, orgID pgtype.UUID) ([]gen.GetExecutorCountsGroupedByOrgRow, error)
}

// ExecutorCountRow represents aggregated executor counts grouped by application, version, and status.
type ExecutorCountRow struct {
	OrganisationID     pgtype.UUID
	ApplicationName    string
	ApplicationVersion string
	Status             string
	Count              int64
}

// Handler serves the OpenMetrics / Prometheus scrape endpoint.
type Handler struct {
	store      Store
	dispatcher Dispatcher
	logger     *slog.Logger
}

// NewHandler creates a new metrics Handler.
func NewHandler(store Store) *Handler {
	return &Handler{store: store, logger: slog.Default()}
}

// NewHandlerWithDispatcher creates a Handler that also aggregates
// workflow and step families via executor dispatch.
func NewHandlerWithDispatcher(store Store, dispatcher Dispatcher) *Handler {
	return &Handler{store: store, dispatcher: dispatcher, logger: slog.Default()}
}

// SetDispatcher attaches or replaces the aggregate dispatcher.
func (h *Handler) SetDispatcher(dispatcher Dispatcher) {
	h.dispatcher = dispatcher
}

// SetLogger attaches a scoped logger for dispatch diagnostics.
func (h *Handler) SetLogger(logger *slog.Logger) {
	if logger != nil {
		h.logger = logger
	}
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

	if !identity.IsAdmin && !auth.HasPermission(perms, auth.PermApplicationRead) && !auth.HasPermission(perms, auth.PermMetricRead) {
		http.Error(w, "forbidden: missing application.read or metric.read permission", http.StatusForbidden)
		return
	}

	appsFilter := r.URL.Query()["applications"]
	workflowNamesFilter := r.URL.Query()["workflow_names"]
	metricsFilter := r.URL.Query()["metrics"]

	isOpenMetrics := strings.Contains(r.Header.Get("Accept"), "application/openmetrics-text")

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

	emitFloatMetric := func(name, help string, entries map[string]floatWithTS) {
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
			e := entries[k]
			if e.hasTS {
				sb.WriteString(fmt.Sprintf("%s%s %s %d\n", name, k, formatMetricValue(e.value), e.timestamp))
			} else {
				sb.WriteString(fmt.Sprintf("%s%s %s\n", name, k, formatMetricValue(e.value)))
			}
		}
	}

	executorCounts := make(map[string]int)

	var rows []ExecutorCountRow
	var queryErr error

	if !identity.IsAdmin {
		if orgStore, ok := h.store.(GenOrgGroupedStore); ok {
			genRows, err := orgStore.GetExecutorCountsGroupedByOrg(r.Context(), identity.OrgID)
			if err != nil {
				queryErr = err
			} else {
				rows = make([]ExecutorCountRow, len(genRows))
				for i, r := range genRows {
					rows[i] = ExecutorCountRow{
						OrganisationID:     r.OrganisationID,
						ApplicationName:    r.ApplicationName,
						ApplicationVersion: r.ApplicationVersion,
						Status:             string(r.Status),
						Count:              r.Count,
					}
				}
			}
		} else if orgStore, ok := h.store.(OrgGroupedExecutorStore); ok {
			rows, queryErr = orgStore.GetExecutorCountsGroupedByOrg(r.Context(), identity.OrgID)
		}
	}

	if rows == nil && queryErr == nil {
		if genStore, ok := h.store.(GenGroupedStore); ok {
			genRows, err := genStore.GetExecutorCountsGrouped(r.Context())
			if err != nil {
				queryErr = err
			} else {
				rows = make([]ExecutorCountRow, len(genRows))
				for i, r := range genRows {
					rows[i] = ExecutorCountRow{
						OrganisationID:     r.OrganisationID,
						ApplicationName:    r.ApplicationName,
						ApplicationVersion: r.ApplicationVersion,
						Status:             string(r.Status),
						Count:              r.Count,
					}
				}
			}
		} else if groupedStore, ok := h.store.(GroupedExecutorStore); ok {
			rows, queryErr = groupedStore.GetExecutorCountsGrouped(r.Context())
		}
	}

	if queryErr != nil {
		http.Error(w, fmt.Sprintf("failed to query grouped executor metrics: %v", queryErr), http.StatusInternalServerError)
		return
	}

	type appRef struct {
		id   pgtype.UUID
		name string
		org  pgtype.UUID
	}
	var scopedApps []appRef
	seenApps := make(map[string]bool)

	collectScopedApp := func(id pgtype.UUID, name string, org pgtype.UUID) {
		if len(appsFilter) > 0 && !contains(appsFilter, name) {
			return
		}
		if !identity.IsAdmin {
			if org.Valid && org != identity.OrgID {
				return
			}
			if len(identity.ApplicationNames) > 0 && !contains(identity.ApplicationNames, name) {
				return
			}
		}
		if seenApps[name] {
			return
		}
		seenApps[name] = true
		scopedApps = append(scopedApps, appRef{id: id, name: name, org: org})
	}

	// The application list below only feeds aggregate dispatch, so skip
	// it on executor-only scrapes.
	wantAppList := h.dispatcher != nil && wantsAnyAggregate(metricsFilter)

	if rows != nil {
		for _, row := range rows {
			if len(appsFilter) > 0 && !contains(appsFilter, row.ApplicationName) {
				continue
			}
			if !identity.IsAdmin {
				if row.OrganisationID.Valid && row.OrganisationID != identity.OrgID {
					continue
				}
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
		// Executor counts came from grouped rows; scoped apps for
		// aggregate dispatch are derived from the application list below.
		if wantAppList {
			apps, err := h.store.ListAllApplications(r.Context())
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to list applications: %v", err), http.StatusInternalServerError)
				return
			}
			for _, app := range apps {
				collectScopedApp(app.ID, app.Name, app.OrganisationID)
			}
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

			if wantAppList {
				collectScopedApp(app.ID, app.Name, app.OrganisationID)
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

	if h.dispatcher != nil && len(scopedApps) > 0 && wantsAnyAggregate(metricsFilter) {
		now := time.Now().UTC()
		// Rates cover the most recently completed clock-aligned minute.
		windowEnd := now.Truncate(time.Minute)
		windowStart := windowEnd.Add(-time.Minute)
		// Rate and windowed series carry the window end as their
		// timestamp so scrapes within the same minute deduplicate.
		// Prometheus text exposition uses milliseconds while
		// OpenMetrics uses seconds.
		var windowTS int64
		if isOpenMetrics {
			windowTS = windowEnd.Unix()
		} else {
			windowTS = windowEnd.UnixMilli()
		}

		agg := newAggregateCollector(windowTS)
		// Aggregate per application concurrently with bounded
		// parallelism so slow executors cannot stall the scrape.
		// Per-request failures degrade to omitted series for that
		// application; executor counts are still served.
		const maxScrapeWorkers = 8
		sem := make(chan struct{}, maxScrapeWorkers)
		var wg sync.WaitGroup
	loop:
		for _, app := range scopedApps {
			select {
			case sem <- struct{}{}:
			case <-r.Context().Done():
				break loop
			}
			wg.Add(1)
			go func(ctx context.Context, a appRef) {
				defer wg.Done()
				defer func() { <-sem }()
				h.collectAppAggregates(ctx, a.name, a.id, windowStart, windowEnd, workflowNamesFilter, metricsFilter, agg)
			}(r.Context(), app)
		}
		wg.Wait()
		agg.emitTo(emitFloatMetric)
	}

	if isOpenMetrics {
		sb.WriteString("# EOF\n")
		w.Header().Set("Content-Type", "application/openmetrics-text; version=1.0.0; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sb.String()))
}

type floatWithTS struct {
	value float64
	// timestamp accompanies rate and windowed series. The unit
	// follows the exposition format: seconds for OpenMetrics
	// (whose timestamps are Unix seconds) and milliseconds for
	// Prometheus text exposition.
	timestamp int64
	hasTS     bool
}

func formatMetricValue(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	if math.Trunc(v) == v && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
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

// workflowLabel builds the label set for workflow families. queue_name is
// always present (possibly empty for workflows that were never enqueued)
// so the label set stays stable across series.
func workflowLabel(app, workflow, queue string) string {
	return fmt.Sprintf(`{application="%s",workflow_name="%s",queue_name="%s"}`, sanitizeLabel(app), sanitizeLabel(workflow), sanitizeLabel(queue))
}

// workflowPendingLabel builds the label set for pending families, which
// upstream labels by workflow only, without a queue dimension.
func workflowPendingLabel(app, workflow string) string {
	return fmt.Sprintf(`{application="%s",workflow_name="%s"}`, sanitizeLabel(app), sanitizeLabel(workflow))
}

func stepLabel(app, step string) string {
	return fmt.Sprintf(`{application="%s",step_name="%s"}`, sanitizeLabel(app), sanitizeLabel(step))
}

func groupString(group map[string]*string, keys ...string) string {
	for _, k := range keys {
		if v, ok := group[k]; ok && v != nil {
			return *v
		}
	}
	return ""
}

type aggregateCollector struct {
	mu         sync.Mutex
	windowTS   int64
	started    map[string]floatWithTS
	dequeued   map[string]floatWithTS
	success    map[string]floatWithTS
	failed     map[string]floatWithTS
	cancelled  map[string]floatWithTS
	enqueued   map[string]floatWithTS
	pending    map[string]floatWithTS
	oldestEnq  map[string]floatWithTS
	oldestPend map[string]floatWithTS
	maxWait    map[string]floatWithTS
	maxLatency map[string]floatWithTS
	stepSucc   map[string]floatWithTS
	stepFail   map[string]floatWithTS
	stepMaxDur map[string]floatWithTS
}

func newAggregateCollector(windowTS int64) *aggregateCollector {
	return &aggregateCollector{
		windowTS:   windowTS,
		started:    make(map[string]floatWithTS),
		dequeued:   make(map[string]floatWithTS),
		success:    make(map[string]floatWithTS),
		failed:     make(map[string]floatWithTS),
		cancelled:  make(map[string]floatWithTS),
		enqueued:   make(map[string]floatWithTS),
		pending:    make(map[string]floatWithTS),
		oldestEnq:  make(map[string]floatWithTS),
		oldestPend: make(map[string]floatWithTS),
		maxWait:    make(map[string]floatWithTS),
		maxLatency: make(map[string]floatWithTS),
		stepSucc:   make(map[string]floatWithTS),
		stepFail:   make(map[string]floatWithTS),
		stepMaxDur: make(map[string]floatWithTS),
	}
}

func (c *aggregateCollector) addRate(dst map[string]floatWithTS, labels string, count int64) {
	if count < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := dst[labels]
	e.value += float64(count) / 60.0
	e.timestamp = c.windowTS
	e.hasTS = true
	dst[labels] = e
}

func (c *aggregateCollector) setPoint(dst map[string]floatWithTS, labels string, value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	dst[labels] = floatWithTS{value: value}
}

func (c *aggregateCollector) setWindowed(dst map[string]floatWithTS, labels string, value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	dst[labels] = floatWithTS{value: value, timestamp: c.windowTS, hasTS: true}
}

func (c *aggregateCollector) emitTo(emit func(name, help string, entries map[string]floatWithTS)) {
	emit("dbos_conductor_v1_workflow_started_rate", "Workflows created per second.", c.started)
	emit("dbos_conductor_v1_workflow_dequeued_rate", "Enqueued workflows dequeued per second.", c.dequeued)
	emit("dbos_conductor_v1_workflow_success_rate", "Workflows that completed successfully per second.", c.success)
	emit("dbos_conductor_v1_workflow_failed_rate", "Workflows that terminated with an error per second.", c.failed)
	emit("dbos_conductor_v1_workflow_cancelled_rate", "Workflows that were cancelled per second.", c.cancelled)
	emit("dbos_conductor_v1_workflow_enqueued_count", "Workflows currently in the ENQUEUED state.", c.enqueued)
	emit("dbos_conductor_v1_workflow_pending_count", "Workflows currently in the PENDING state.", c.pending)
	emit("dbos_conductor_v1_workflow_oldest_enqueued_timestamp_seconds", "Unix timestamp of the oldest ENQUEUED workflow.", c.oldestEnq)
	emit("dbos_conductor_v1_workflow_oldest_pending_timestamp_seconds", "Unix timestamp of the oldest PENDING workflow.", c.oldestPend)
	emit("dbos_conductor_v1_workflow_max_queue_wait_seconds", "Maximum queue wait across workflows completed successfully in the window.", c.maxWait)
	emit("dbos_conductor_v1_workflow_max_total_latency_seconds", "Maximum end-to-end latency across workflows completed successfully in the window.", c.maxLatency)
	emit("dbos_conductor_v1_step_success_rate", "Workflow steps that completed successfully per second.", c.stepSucc)
	emit("dbos_conductor_v1_step_failed_rate", "Workflow steps that terminated with an error per second.", c.stepFail)
	emit("dbos_conductor_v1_step_max_duration_seconds", "Maximum single-step duration across steps completed successfully in the window.", c.stepMaxDur)
}

func wantsMetric(metricsFilter []string, names ...string) bool {
	if len(metricsFilter) == 0 {
		return true
	}
	for _, n := range names {
		if contains(metricsFilter, n) {
			return true
		}
	}
	return false
}

// aggregateFamilies names every workflow and step family served from
// executor dispatch, used to skip that work on executor-only scrapes.
var aggregateFamilies = []string{
	"dbos_conductor_v1_workflow_started_rate",
	"dbos_conductor_v1_workflow_dequeued_rate",
	"dbos_conductor_v1_workflow_success_rate",
	"dbos_conductor_v1_workflow_failed_rate",
	"dbos_conductor_v1_workflow_cancelled_rate",
	"dbos_conductor_v1_workflow_enqueued_count",
	"dbos_conductor_v1_workflow_pending_count",
	"dbos_conductor_v1_workflow_oldest_enqueued_timestamp_seconds",
	"dbos_conductor_v1_workflow_oldest_pending_timestamp_seconds",
	"dbos_conductor_v1_workflow_max_queue_wait_seconds",
	"dbos_conductor_v1_workflow_max_total_latency_seconds",
	"dbos_conductor_v1_step_success_rate",
	"dbos_conductor_v1_step_failed_rate",
	"dbos_conductor_v1_step_max_duration_seconds",
}

func wantsAnyAggregate(metricsFilter []string) bool {
	return wantsMetric(metricsFilter, aggregateFamilies...)
}

func (h *Handler) collectAppAggregates(ctx context.Context, appName string, appID pgtype.UUID, windowStart, windowEnd time.Time, workflowNamesFilter, metricsFilter []string, agg *aggregateCollector) {
	needRates := wantsMetric(metricsFilter,
		"dbos_conductor_v1_workflow_started_rate",
		"dbos_conductor_v1_workflow_dequeued_rate",
		"dbos_conductor_v1_workflow_success_rate",
		"dbos_conductor_v1_workflow_failed_rate",
		"dbos_conductor_v1_workflow_cancelled_rate",
		"dbos_conductor_v1_workflow_max_queue_wait_seconds",
		"dbos_conductor_v1_workflow_max_total_latency_seconds")
	needSnapshot := wantsMetric(metricsFilter,
		"dbos_conductor_v1_workflow_enqueued_count",
		"dbos_conductor_v1_workflow_pending_count",
		"dbos_conductor_v1_workflow_oldest_enqueued_timestamp_seconds",
		"dbos_conductor_v1_workflow_oldest_pending_timestamp_seconds")
	needSteps := wantsMetric(metricsFilter,
		"dbos_conductor_v1_step_success_rate",
		"dbos_conductor_v1_step_failed_rate",
		"dbos_conductor_v1_step_max_duration_seconds")

	if !needRates && !needSnapshot && !needSteps {
		return
	}

	var nameFilter protocol.StringOrList
	if len(workflowNamesFilter) > 0 {
		nameFilter = protocol.StringOrList(workflowNamesFilter)
	}

	dispatchWithTimeout := func(msg protocol.Message) (protocol.Message, error) {
		dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		resp, err := h.dispatcher.Dispatch(dctx, appID, msg)
		if err != nil {
			h.logger.Warn("aggregate dispatch failed",
				"app", appName,
				"type", string(msg.GetMessageType()),
				"error", err,
			)
		}
		return resp, err
	}

	if needRates && wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_started_rate") {
		req := &protocol.GetWorkflowAggregatesRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: uuid.NewString()},
			Body: protocol.GetWorkflowAggregatesRequestBody{
				GroupByName:      true,
				GroupByQueueName: true,
				SelectCount:      true,
				StartTime:        &windowStart,
				EndTime:          &windowEnd,
				Name:             nameFilter,
			},
		}
		if respMsg, err := dispatchWithTimeout(req); err == nil {
			if resp, ok := respMsg.(*protocol.GetWorkflowAggregatesResponse); ok {
				for _, row := range resp.Output {
					if row.Count == nil {
						continue
					}
					wf := groupString(row.Group, "workflow_name", "name")
					q := groupString(row.Group, "queue_name", "queue")
					if wf == "" {
						continue
					}
					if len(workflowNamesFilter) > 0 && !contains(workflowNamesFilter, wf) {
						continue
					}
					agg.addRate(agg.started, workflowLabel(appName, wf, q), *row.Count)
				}
			}
		}
	}

	if needRates && wantsMetric(metricsFilter,
		"dbos_conductor_v1_workflow_success_rate",
		"dbos_conductor_v1_workflow_failed_rate",
		"dbos_conductor_v1_workflow_cancelled_rate",
		"dbos_conductor_v1_workflow_max_queue_wait_seconds",
		"dbos_conductor_v1_workflow_max_total_latency_seconds") {
		req := &protocol.GetWorkflowAggregatesRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: uuid.NewString()},
			Body: protocol.GetWorkflowAggregatesRequestBody{
				GroupByStatus:           true,
				GroupByName:             true,
				GroupByQueueName:        true,
				SelectCount:             true,
				SelectMaxQueueWaitMs:    true,
				SelectMaxTotalLatencyMs: true,
				CompletedAfter:          &windowStart,
				CompletedBefore:         &windowEnd,
				Name:                    nameFilter,
			},
		}
		if respMsg, err := dispatchWithTimeout(req); err == nil {
			if resp, ok := respMsg.(*protocol.GetWorkflowAggregatesResponse); ok {
				for _, row := range resp.Output {
					wf := groupString(row.Group, "workflow_name", "name")
					q := groupString(row.Group, "queue_name", "queue")
					status := strings.ToUpper(groupString(row.Group, "status"))
					if wf == "" {
						continue
					}
					if len(workflowNamesFilter) > 0 && !contains(workflowNamesFilter, wf) {
						continue
					}
					labels := workflowLabel(appName, wf, q)
					if row.Count != nil {
						switch status {
						case "SUCCESS":
							if wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_success_rate") {
								agg.addRate(agg.success, labels, *row.Count)
							}
						case "ERROR", "MAX_RECOVERY_ATTEMPTS_EXCEEDED":
							if wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_failed_rate") {
								agg.addRate(agg.failed, labels, *row.Count)
							}
						case "CANCELLED", "CANCELED":
							if wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_cancelled_rate") {
								agg.addRate(agg.cancelled, labels, *row.Count)
							}
						}
					}
					if status == "SUCCESS" || status == "" {
						if row.MaxQueueWaitMs != nil && wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_max_queue_wait_seconds") {
							agg.setWindowed(agg.maxWait, labels, float64(*row.MaxQueueWaitMs)/1000.0)
						}
						if row.MaxTotalLatencyMs != nil && wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_max_total_latency_seconds") {
							agg.setWindowed(agg.maxLatency, labels, float64(*row.MaxTotalLatencyMs)/1000.0)
						}
					}
				}
			}
		}
	}

	if needRates && wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_dequeued_rate") {
		req := &protocol.GetWorkflowAggregatesRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: uuid.NewString()},
			Body: protocol.GetWorkflowAggregatesRequestBody{
				GroupByName:      true,
				GroupByQueueName: true,
				SelectCount:      true,
				DequeuedAfter:    &windowStart,
				DequeuedBefore:   &windowEnd,
				Name:             nameFilter,
			},
		}
		if respMsg, err := dispatchWithTimeout(req); err == nil {
			if resp, ok := respMsg.(*protocol.GetWorkflowAggregatesResponse); ok {
				for _, row := range resp.Output {
					if row.Count == nil {
						continue
					}
					wf := groupString(row.Group, "workflow_name", "name")
					q := groupString(row.Group, "queue_name", "queue")
					if wf == "" {
						continue
					}
					if len(workflowNamesFilter) > 0 && !contains(workflowNamesFilter, wf) {
						continue
					}
					agg.addRate(agg.dequeued, workflowLabel(appName, wf, q), *row.Count)
				}
			}
		}
	}

	if needSnapshot && wantsMetric(metricsFilter,
		"dbos_conductor_v1_workflow_enqueued_count",
		"dbos_conductor_v1_workflow_oldest_enqueued_timestamp_seconds") {
		req := &protocol.GetWorkflowAggregatesRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: uuid.NewString()},
			Body: protocol.GetWorkflowAggregatesRequestBody{
				GroupByName:        true,
				GroupByQueueName:   true,
				SelectCount:        true,
				SelectMinCreatedAt: true,
				Status:             protocol.StringOrList{"ENQUEUED"},
				Name:               nameFilter,
			},
		}
		if respMsg, err := dispatchWithTimeout(req); err == nil {
			if resp, ok := respMsg.(*protocol.GetWorkflowAggregatesResponse); ok {
				for _, row := range resp.Output {
					wf := groupString(row.Group, "workflow_name", "name")
					q := groupString(row.Group, "queue_name", "queue")
					if wf == "" {
						continue
					}
					if len(workflowNamesFilter) > 0 && !contains(workflowNamesFilter, wf) {
						continue
					}
					labels := workflowLabel(appName, wf, q)
					if row.Count != nil && wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_enqueued_count") {
						agg.setPoint(agg.enqueued, labels, float64(*row.Count))
					}
					if row.MinCreatedAt != nil && wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_oldest_enqueued_timestamp_seconds") {
						agg.setPoint(agg.oldestEnq, labels, float64(*row.MinCreatedAt)/1000.0)
					}
				}
			}
		}
	}

	if needSnapshot && wantsMetric(metricsFilter,
		"dbos_conductor_v1_workflow_pending_count",
		"dbos_conductor_v1_workflow_oldest_pending_timestamp_seconds") {
		req := &protocol.GetWorkflowAggregatesRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: uuid.NewString()},
			Body: protocol.GetWorkflowAggregatesRequestBody{
				GroupByName:        true,
				SelectCount:        true,
				SelectMinCreatedAt: true,
				Status:             protocol.StringOrList{"PENDING"},
				Name:               nameFilter,
			},
		}
		if respMsg, err := dispatchWithTimeout(req); err == nil {
			if resp, ok := respMsg.(*protocol.GetWorkflowAggregatesResponse); ok {
				for _, row := range resp.Output {
					wf := groupString(row.Group, "workflow_name", "name")
					if wf == "" {
						continue
					}
					if len(workflowNamesFilter) > 0 && !contains(workflowNamesFilter, wf) {
						continue
					}
					labels := workflowPendingLabel(appName, wf)
					if row.Count != nil && wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_pending_count") {
						agg.setPoint(agg.pending, labels, float64(*row.Count))
					}
					if row.MinCreatedAt != nil && wantsMetric(metricsFilter, "dbos_conductor_v1_workflow_oldest_pending_timestamp_seconds") {
						agg.setPoint(agg.oldestPend, labels, float64(*row.MinCreatedAt)/1000.0)
					}
				}
			}
		}
	}

	if needSteps {
		// workflow_names selects workflow families only. Step series are
		// labeled by step_name, a separate namespace, so they are left
		// unfiltered here rather than matched against workflow names.
		req := &protocol.GetStepAggregatesRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetStepAggregates, RequestID: uuid.NewString()},
			Body: protocol.GetStepAggregatesRequestBody{
				GroupByFunctionName: true,
				GroupByStatus:       true,
				SelectCount:         true,
				SelectMaxDurationMs: true,
				CompletedAfter:      &windowStart,
				CompletedBefore:     &windowEnd,
			},
		}
		if respMsg, err := dispatchWithTimeout(req); err == nil {
			if resp, ok := respMsg.(*protocol.GetStepAggregatesResponse); ok {
				for _, row := range resp.Output {
					step := groupString(row.Group, "function_name", "step_name", "name")
					status := strings.ToUpper(groupString(row.Group, "status"))
					if step == "" {
						continue
					}
					labels := stepLabel(appName, step)
					if row.Count != nil {
						if status == "SUCCESS" {
							if wantsMetric(metricsFilter, "dbos_conductor_v1_step_success_rate") {
								agg.addRate(agg.stepSucc, labels, *row.Count)
							}
						} else if status != "" {
							// Any classified non-success status counts as an
							// error. Rows with an empty status cannot be
							// classified and are dropped.
							if wantsMetric(metricsFilter, "dbos_conductor_v1_step_failed_rate") {
								agg.addRate(agg.stepFail, labels, *row.Count)
							}
						}
					}
					if status == "SUCCESS" && row.MaxDurationMs != nil && wantsMetric(metricsFilter, "dbos_conductor_v1_step_max_duration_seconds") {
						agg.setWindowed(agg.stepMaxDur, labels, float64(*row.MaxDurationMs)/1000.0)
					}
				}
			}
		}
	}
}
