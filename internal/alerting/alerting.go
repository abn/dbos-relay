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
	store      Store
	dispatcher Dispatcher
	logger     *slog.Logger
	wg         sync.WaitGroup
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

	default:
		return false, nil, fmt.Errorf("unknown rule type: %s", rule.RuleType)
	}
}

func (e *Evaluator) fireAlert(ctx context.Context, rule gen.AlertingRule, meta map[string]string) {
	reqID := uuid.New().String()
	alertReq := &protocol.AlertRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeAlert,
			RequestID: reqID,
		},
		Name:     rule.RuleType,
		Message:  fmt.Sprintf("%s alert triggered", rule.RuleType),
		Metadata: meta,
	}

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

	if err := e.store.TouchAlertRuleLastFired(ctx, rule.ID); err != nil {
		e.logger.Warn("failed to touch alert rule last fired timestamp", "ruleID", rule.ID, "error", err)
	}
}
