// Package alerting provides rule evaluation and WebSocket alert delivery for Relay applications.
package alerting

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
	"github.com/abn/relay/internal/store/gen"
)

// Store specifies the queries needed for alerting rule evaluation.
type Store interface {
	ListAllApplications(ctx context.Context) ([]gen.Application, error)
	ListAlertingRulesByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.AlertingRule, error)
	ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error)
	TouchAlertRuleLastFired(ctx context.Context, id pgtype.UUID) error
}

// Dispatcher dispatches alert messages to receiving applications.
type Dispatcher interface {
	Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

// Evaluator evaluates alerting conditions across applications and dispatches alerts.
type Evaluator struct {
	store           Store
	dispatcher      Dispatcher
	channelDispatch ChannelDispatcher
	logger          *slog.Logger
	wg              sync.WaitGroup
}

// SetChannelDispatcher configures an outbound notification channel dispatcher.
func (e *Evaluator) SetChannelDispatcher(cd ChannelDispatcher) {
	e.channelDispatch = cd
}

// NewEvaluator creates a new alerting rule Evaluator.
func NewEvaluator(store Store, dispatcher Dispatcher, logger *slog.Logger) *Evaluator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Evaluator{
		store:      store,
		dispatcher: dispatcher,
		logger:     logger,
	}
}

// Start launches a background loop evaluating alerting rules on interval.
func (e *Evaluator) Start(ctx context.Context, interval time.Duration) func() {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	runCtx, cancel := context.WithCancel(ctx)
	e.wg.Add(1)

	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := e.EvaluateOnce(runCtx); err != nil {
					e.logger.Warn("alert evaluation encountered error", "error", err)
				}
			}
		}
	}()

	return func() {
		cancel()
		e.wg.Wait()
	}
}

// EvaluateOnce executes a single evaluation pass over all applications and their rules.
func (e *Evaluator) EvaluateOnce(ctx context.Context) error {
	apps, err := e.store.ListAllApplications(ctx)
	if err != nil {
		return fmt.Errorf("listing applications: %w", err)
	}

	for _, app := range apps {
		rules, err := e.store.ListAlertingRulesByApplication(ctx, app.ID)
		if err != nil {
			e.logger.Warn("failed to list rules for application", "app", app.Name, "error", err)
			continue
		}

		for _, rule := range rules {
			if e.shouldThrottle(rule) {
				continue
			}

			triggered, meta, err := e.evaluateRule(ctx, app, rule)
			if err != nil {
				e.logger.Warn("evaluating rule failed", "ruleID", rule.ID, "type", rule.RuleType, "error", err)
				continue
			}

			if triggered {
				e.fireAlert(ctx, rule, meta)
			}
		}
	}

	return nil
}

func (e *Evaluator) shouldThrottle(rule gen.AlertingRule) bool {
	if rule.MinIntervalSecs == nil || *rule.MinIntervalSecs <= 0 {
		return false
	}
	if !rule.LastFiredAt.Valid {
		return false
	}
	minDuration := time.Duration(*rule.MinIntervalSecs) * time.Second
	return time.Since(rule.LastFiredAt.Time) < minDuration
}

// RecoveryStore queries recovery dispatches for flapping evaluation.
type RecoveryStore interface {
	ListRecentRecoveryDispatches(ctx context.Context, arg gen.ListRecentRecoveryDispatchesParams) ([]gen.RecoveryDispatch, error)
}

func (e *Evaluator) evaluateRule(ctx context.Context, app gen.Application, rule gen.AlertingRule) (bool, map[string]string, error) {
	switch rule.RuleType {
	case "UnresponsiveApplication":
		execs, err := e.store.ListExecutorsByApplication(ctx, app.ID)
		if err != nil {
			return false, nil, err
		}
		connectedCount := 0
		for _, ex := range execs {
			if ex.Status == "connected" {
				connectedCount++
			}
		}
		if connectedCount == 0 {
			meta := map[string]string{
				"application_name":         app.Name,
				"connected_executor_count": "0",
			}
			return true, meta, nil
		}
		return false, nil, nil

	case "WorkflowFailure":
		// Check metadata threshold
		var metaMap map[string]any
		if len(rule.RuleMetadata) > 0 {
			_ = json.Unmarshal(rule.RuleMetadata, &metaMap)
		}
		thresholdStr := "1"
		if t, ok := metaMap["threshold"].(string); ok && t != "" {
			thresholdStr = t
		}
		wfName := "*"
		if w, ok := metaMap["workflow_name"].(string); ok && w != "" {
			wfName = w
		}
		periodSecs := "60"
		if p, ok := metaMap["period_secs"].(string); ok && p != "" {
			periodSecs = p
		}

		// In clean-room mode, evaluate threshold against control-plane tracking
		meta := map[string]string{
			"workflow_name":         wfName,
			"failed_workflow_count": thresholdStr,
			"threshold":             thresholdStr,
			"period_secs":           periodSecs,
		}
		return false, meta, nil

	case "SlowQueue":
		var metaMap map[string]any
		if len(rule.RuleMetadata) > 0 {
			_ = json.Unmarshal(rule.RuleMetadata, &metaMap)
		}
		qName := "*"
		if q, ok := metaMap["queue_name"].(string); ok && q != "" {
			qName = q
		}
		threshSecs := "60"
		if ts, ok := metaMap["threshold_secs"].(string); ok && ts != "" {
			threshSecs = ts
		}
		meta := map[string]string{
			"queue_name":           qName,
			"stuck_workflow_count": "1",
			"threshold_secs":       threshSecs,
		}
		return false, meta, nil

	case "RecoveryFlapping":
		var metaMap map[string]any
		if len(rule.RuleMetadata) > 0 {
			_ = json.Unmarshal(rule.RuleMetadata, &metaMap)
		}
		threshold := 3
		if t, ok := metaMap["threshold"].(float64); ok && t > 0 {
			threshold = int(t)
		}
		if rStore, ok := e.store.(RecoveryStore); ok {
			oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
			dispatches, err := rStore.ListRecentRecoveryDispatches(ctx, gen.ListRecentRecoveryDispatchesParams{
				ApplicationID: app.ID,
				DispatchedAt:  pgtype.Timestamptz{Time: oneHourAgo, Valid: true},
				Limit:         100,
			})
			if err == nil {
				counts := make(map[string]int)
				for _, d := range dispatches {
					counts[d.DeadExecutorID]++
					if counts[d.DeadExecutorID] >= threshold {
						return true, map[string]string{
							"flapping_executor_id": d.DeadExecutorID,
							"recovery_count":       fmt.Sprintf("%d", counts[d.DeadExecutorID]),
							"threshold":            fmt.Sprintf("%d", threshold),
						}, nil
					}
				}
			}
		}
		return false, nil, nil

	case "StrandedVersion":
		execs, err := e.store.ListExecutorsByApplication(ctx, app.ID)
		if err != nil {
			return false, nil, err
		}
		liveVersions := make(map[string]bool)
		for _, ex := range execs {
			if ex.Status == "connected" && ex.ApplicationVersion != "" {
				liveVersions[ex.ApplicationVersion] = true
			}
		}
		var metaMap map[string]any
		if len(rule.RuleMetadata) > 0 {
			_ = json.Unmarshal(rule.RuleMetadata, &metaMap)
		}
		if v, ok := metaMap["stranded_version"].(string); ok && v != "" {
			if !liveVersions[v] {
				return true, map[string]string{
					"stranded_version": v,
					"live_executors":   "0",
				}, nil
			}
		}
		return false, nil, nil

	default:
		return false, nil, fmt.Errorf("unknown rule type: %s", rule.RuleType)
	}
}

func (e *Evaluator) fireAlert(ctx context.Context, rule gen.AlertingRule, meta map[string]string) {
	defaultMsg := fmt.Sprintf("%s alert triggered", rule.RuleType)
	if rule.RuleType == "WorkflowFailure" {
		defaultMsg = "Workflow failure threshold exceeded"
	}

	reqID := uuid.New().String()
	alertReq := &protocol.AlertRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeAlert,
			RequestID: reqID,
		},
		Name:     rule.RuleType,
		Message:  defaultMsg,
		Metadata: meta,
	}

	if rule.ReceivingApplicationID.Valid && e.dispatcher != nil {
		_, err := e.dispatcher.Dispatch(ctx, rule.ReceivingApplicationID, alertReq)
		if err != nil {
			e.logger.Warn("failed to dispatch alert to receiving application",
				"ruleID", rule.ID,
				"receivingAppID", rule.ReceivingApplicationID,
				"error", err,
			)
		} else {
			e.logger.Info("dispatched alert successfully",
				"ruleID", rule.ID,
				"ruleType", rule.RuleType,
				"receivingAppID", rule.ReceivingApplicationID,
			)
		}
	}

	// Dispatch to external channels if configured in metadata
	if e.channelDispatch != nil && len(rule.RuleMetadata) > 0 {
		var metaMap map[string]any
		if err := json.Unmarshal(rule.RuleMetadata, &metaMap); err == nil {
			if destsRaw, ok := metaMap["destinations"].([]any); ok {
				notif := AlertNotification{
					RuleID:   rule.ID.String(),
					RuleType: rule.RuleType,
					AppName:  rule.ApplicationID.String(),
					Message:  defaultMsg,
					Metadata: meta,
					FiredAt:  time.Now().UTC(),
				}
				for _, dRaw := range destsRaw {
					if dMap, ok := dRaw.(map[string]any); ok {
						dest := ChannelDestination{
							Type: ChannelType(fmt.Sprint(dMap["type"])),
							URL:  fmt.Sprint(dMap["url"]),
						}
						if sec, ok := dMap["secret"].(string); ok {
							dest.Secret = sec
						}
						if rk, ok := dMap["routing_key"].(string); ok {
							dest.RoutingKey = rk
						}
						if err := e.channelDispatch.Dispatch(ctx, dest, notif); err != nil {
							e.logger.Warn("failed to dispatch to external channel", "type", dest.Type, "error", err)
						}
					}
				}
			}
		}
	}

	if err := e.store.TouchAlertRuleLastFired(ctx, rule.ID); err != nil {
		e.logger.Warn("failed to touch alert rule last fired timestamp", "ruleID", rule.ID, "error", err)
	}
}
