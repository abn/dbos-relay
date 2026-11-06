package liveness

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

const (
	DefaultProbeInterval = 10 * time.Second
	DefaultProbeTTL      = 30 * time.Second
	probeSweepInterval   = 5 * time.Second
)

// ProbeStore resolves an application and records its heartbeat row.
type ProbeStore interface {
	GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error)
	GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error)
	UpsertExecutor(ctx context.Context, arg gen.UpsertExecutorParams) (gen.Executor, error)
}

// ProbeSink receives the liveness transitions produced by a probe.
type ProbeSink interface {
	OnProbeConnect(ctx context.Context, appID pgtype.UUID, executorID, version string) error
	OnProbeDisconnect(ctx context.Context, appID pgtype.UUID, executorID string)
}

// ProbePolicy decides whether an application accepts probe heartbeats. Only
// Direct mode does; an Attached application has a socket heartbeat already.
type ProbePolicy interface {
	AllowProbe(appID pgtype.UUID) bool
}

type probeEntry struct {
	appID      pgtype.UUID
	executorID string
	expiresAt  time.Time
}

// ProbeRegistry tracks the last heartbeat for each probed executor and turns a
// missed heartbeat into a disconnect event after the TTL expires.
type ProbeRegistry struct {
	mu      sync.Mutex
	entries map[string]probeEntry
	sink    ProbeSink
	logger  *slog.Logger
	now     func() time.Time
}

// NewProbeRegistry creates a probe registry.
func NewProbeRegistry(sink ProbeSink, logger *slog.Logger) *ProbeRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProbeRegistry{
		entries: make(map[string]probeEntry),
		sink:    sink,
		logger:  logger,
		now:     time.Now,
	}
}

func probeKey(appID pgtype.UUID, executorID string) string {
	return fmt.Sprintf("%x:%s", appID.Bytes, executorID)
}

// Touch records a heartbeat for an executor and reports whether this is the
// first heartbeat in the current registry lifetime.
func (r *ProbeRegistry) Touch(appID pgtype.UUID, executorID string, ttl time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := probeKey(appID, executorID)
	_, existed := r.entries[key]
	r.entries[key] = probeEntry{
		appID:      appID,
		executorID: executorID,
		expiresAt:  r.now().Add(ttl),
	}
	return !existed
}

// Remove forgets an executor that reported a graceful stop.
func (r *ProbeRegistry) Remove(appID pgtype.UUID, executorID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, probeKey(appID, executorID))
}

// Sweep removes and returns entries whose heartbeat has expired.
func (r *ProbeRegistry) Sweep(now time.Time) []probeEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	var expired []probeEntry
	for key, entry := range r.entries {
		if now.After(entry.expiresAt) {
			expired = append(expired, entry)
			delete(r.entries, key)
		}
	}
	return expired
}

// Run sweeps expired probes until the context is cancelled.
func (r *ProbeRegistry) Run(ctx context.Context) {
	ticker := time.NewTicker(probeSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			for _, entry := range r.Sweep(now) {
				r.logger.Warn("probe heartbeat expired, marking executor disconnected",
					"executorID", entry.executorID,
				)
				r.sink.OnProbeDisconnect(ctx, entry.appID, entry.executorID)
			}
		}
	}
}

// ProbeHandler receives heartbeats from relay-probe.
type ProbeHandler struct {
	store      ProbeStore
	sink       ProbeSink
	registry   *ProbeRegistry
	policy     ProbePolicy
	secret     string
	defaultOrg string
	instanceID pgtype.UUID
	logger     *slog.Logger
	now        func() time.Time
}

// NewProbeHandler creates a probe HTTP handler.
func NewProbeHandler(store ProbeStore, sink ProbeSink, registry *ProbeRegistry, policy ProbePolicy, secret, defaultOrg string, instanceID pgtype.UUID, logger *slog.Logger) *ProbeHandler {
	if defaultOrg == "" {
		defaultOrg = "default"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ProbeHandler{
		store:      store,
		sink:       sink,
		registry:   registry,
		policy:     policy,
		secret:     secret,
		defaultOrg: defaultOrg,
		instanceID: instanceID,
		logger:     logger,
		now:        time.Now,
	}
}

type probeRequest struct {
	Organisation       string `json:"organisation,omitempty"`
	Application        string `json:"application"`
	ExecutorID         string `json:"executor_id"`
	ApplicationVersion string `json:"application_version,omitempty"`
	Hostname           string `json:"hostname,omitempty"`
	TTLSeconds         int    `json:"ttl_seconds,omitempty"`
	Stop               bool   `json:"stop,omitempty"`
}

func (h *ProbeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req probeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "invalid probe body", http.StatusBadRequest)
		return
	}
	if req.Application == "" || req.ExecutorID == "" {
		http.Error(w, "application and executor_id are required", http.StatusBadRequest)
		return
	}
	org, app, err := h.resolve(r.Context(), req.Organisation, req.Application)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		h.logger.Warn("probe could not resolve application", "application", req.Application, "error", err)
		http.Error(w, http.StatusText(status), status)
		return
	}
	if h.policy != nil && !h.policy.AllowProbe(app.ID) {
		http.Error(w, "application is not configured for Direct mode", http.StatusForbidden)
		return
	}
	if req.Stop {
		h.sink.OnProbeDisconnect(r.Context(), app.ID, req.ExecutorID)
		h.registry.Remove(app.ID, req.ExecutorID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = DefaultProbeTTL
	}
	if ttl > 5*time.Minute {
		ttl = 5 * time.Minute
	}
	expires := h.now().Add(ttl)
	owner := pgtype.UUID{}
	if h.instanceID.Valid {
		owner = h.instanceID
	}
	if _, err := h.store.UpsertExecutor(r.Context(), gen.UpsertExecutorParams{
		ApplicationID:      app.ID,
		ExecutorID:         req.ExecutorID,
		ApplicationVersion: req.ApplicationVersion,
		Hostname:           req.Hostname,
		Metadata:           []byte("{}"),
		OwnerInstanceID:    owner,
		LeaseExpiresAt:     pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		h.logger.Error("failed to record probe heartbeat", "application", org.Name+"/"+app.Name, "error", err)
		http.Error(w, "failed to record heartbeat", http.StatusInternalServerError)
		return
	}
	if h.registry.Touch(app.ID, req.ExecutorID, ttl) {
		if err := h.sink.OnProbeConnect(r.Context(), app.ID, req.ExecutorID, req.ApplicationVersion); err != nil {
			h.logger.Warn("probe connect transition failed", "executorID", req.ExecutorID, "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProbeHandler) authorized(r *http.Request) bool {
	if h.secret == "" {
		return false
	}
	got := r.Header.Get("X-Relay-Internal-Secret")
	return subtle.ConstantTimeCompare([]byte(got), []byte(h.secret)) == 1
}

func (h *ProbeHandler) resolve(ctx context.Context, orgName, appName string) (gen.Organisation, gen.Application, error) {
	if orgName == "" {
		orgName = h.defaultOrg
	}
	candidates := []string{orgName}
	if orgName != "default" {
		candidates = append(candidates, "default")
	}
	if orgName != "local" {
		candidates = append(candidates, "local")
	}
	var lastErr error
	for _, candidate := range candidates {
		org, err := h.store.GetOrganisationByName(ctx, candidate)
		if err != nil {
			lastErr = err
			continue
		}
		app, err := h.store.GetApplicationByName(ctx, gen.GetApplicationByNameParams{
			OrganisationID: org.ID,
			Name:           appName,
		})
		if err != nil {
			lastErr = err
			continue
		}
		return org, app, nil
	}
	if lastErr == nil {
		lastErr = pgx.ErrNoRows
	}
	return gen.Organisation{}, gen.Application{}, lastErr
}
