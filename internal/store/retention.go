package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/safego"
	"github.com/abn/relay/internal/store/gen"
)

// ApplicationRetentionSettings represents the subset of application settings governing workflow retention and GC.
type ApplicationRetentionSettings struct {
	PrivateMode         bool    `json:"privateMode,omitempty"`
	ExecutorTimeoutSecs int64   `json:"executorTimeoutSecs,omitempty"`
	GcRowsThreshold     *int64  `json:"gcRowsThreshold,omitempty"`
	GcTimeThresholdMs   *int64  `json:"gcTimeThresholdMs,omitempty"`
	GlobalTimeoutMs     *int64  `json:"globalTimeoutMs,omitempty"`
	LatestVersion       *string `json:"latestVersion,omitempty"`
}

// BuildRetentionRequest inspects application settings and builds a retention request if policies are active.
func BuildRetentionRequest(settingsBytes []byte) (*protocol.RetentionRequest, bool) {
	if len(settingsBytes) == 0 {
		return nil, false
	}

	var settings ApplicationRetentionSettings
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		return nil, false
	}

	if settings.GcRowsThreshold == nil && settings.GcTimeThresholdMs == nil && settings.GlobalTimeoutMs == nil {
		return nil, false
	}

	nowMs := time.Now().UnixMilli()
	body := protocol.RetentionRequestBody{}

	if settings.GcRowsThreshold != nil {
		rows := int(*settings.GcRowsThreshold)
		body.GCRowsThreshold = &rows
	}
	if settings.GcTimeThresholdMs != nil {
		cutoff := int(nowMs - *settings.GcTimeThresholdMs)
		body.GCCutoffEpochMs = &cutoff
	}
	if settings.GlobalTimeoutMs != nil {
		cutoff := int(nowMs - *settings.GlobalTimeoutMs)
		body.TimeoutCutoffEpochMs = &cutoff
	}
	if settings.GcRowsThreshold != nil || settings.GcTimeThresholdMs != nil {
		batchSize := 1000
		body.GCBatchSize = &batchSize
	}

	req := &protocol.RetentionRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeRetention,
			RequestID: uuid.NewString(),
		},
		Body: body,
	}

	return req, true
}

// RetentionDispatcher dispatches retention wire messages to connected executors for an application.
type RetentionDispatcher interface {
	Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

// ApplicationRetentionReader provides application queries needed for retention policy scheduling.
type ApplicationRetentionReader interface {
	ListAllApplications(ctx context.Context) ([]gen.Application, error)
	GetApplicationByID(ctx context.Context, id pgtype.UUID) (gen.Application, error)
}

// RetentionScheduler periodically evaluates retention policies and executes connect-time dispatch.
type RetentionScheduler struct {
	store      ApplicationRetentionReader
	dispatcher RetentionDispatcher
	logger     *slog.Logger
	wg         sync.WaitGroup
}

// NewRetentionScheduler constructs a new retention policy scheduler.
func NewRetentionScheduler(store ApplicationRetentionReader, dispatcher RetentionDispatcher, logger *slog.Logger) *RetentionScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &RetentionScheduler{
		store:      store,
		dispatcher: dispatcher,
		logger:     logger,
	}
}

// DispatchConnectRetention dispatches workflow retention policies when an executor connects.
func (s *RetentionScheduler) DispatchConnectRetention(ctx context.Context, appID pgtype.UUID) error {
	if s.store == nil || s.dispatcher == nil {
		return nil
	}

	app, err := s.store.GetApplicationByID(ctx, appID)
	if err != nil {
		return fmt.Errorf("fetching application for connect retention: %w", err)
	}

	req, hasRetention := BuildRetentionRequest(app.Settings)
	if !hasRetention {
		return nil
	}

	if _, err := s.dispatcher.Dispatch(ctx, appID, req); err != nil {
		s.logger.Warn("connect retention dispatch failed", "appID", appID, "appName", app.Name, "error", err)
		return fmt.Errorf("dispatching connect retention: %w", err)
	}

	s.logger.Debug("dispatched connect retention policy", "appID", appID, "appName", app.Name)
	return nil
}

// RunOnce runs a single pass across all applications, dispatching retention requests where configured.
func (s *RetentionScheduler) RunOnce(ctx context.Context) error {
	if s.store == nil || s.dispatcher == nil {
		return nil
	}

	apps, err := s.store.ListAllApplications(ctx)
	if err != nil {
		return fmt.Errorf("listing applications for retention: %w", err)
	}

	for _, app := range apps {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		req, hasRetention := BuildRetentionRequest(app.Settings)
		if !hasRetention {
			continue
		}

		appCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := s.dispatcher.Dispatch(appCtx, app.ID, req)
		cancel()

		if err != nil {
			s.logger.Warn("scheduled retention dispatch failed", "appID", app.ID, "appName", app.Name, "error", err)
		} else {
			s.logger.Debug("dispatched scheduled retention policy", "appID", app.ID, "appName", app.Name)
		}
	}

	return nil
}

// Start launches the background scheduler loop running at the specified interval.
func (s *RetentionScheduler) Start(ctx context.Context, interval time.Duration) func() {
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.wg.Add(1)

	safego.Go(s.logger, "retention-scheduler", func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := s.RunOnce(runCtx); err != nil {
					s.logger.Warn("retention scheduler pass encountered error", "error", err)
				}
			}
		}
	})

	return func() {
		cancel()
		s.wg.Wait()
	}
}
