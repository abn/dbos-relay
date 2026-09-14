// Package alerting provides rule evaluation and WebSocket alert delivery for Relay applications.
package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/safego"
	"github.com/abn/relay/internal/store/gen"
)

// Store specifies the queries needed for alerting rule evaluation.
type Store interface {
	ListAllApplications(ctx context.Context) ([]gen.Application, error)
	ListAlertingRulesByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.AlertingRule, error)
	ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error)
	TouchAlertRuleLastFired(ctx context.Context, id pgtype.UUID) error
}

// ClaimAlertRuleStore is an optional interface for stores that support atomic alert rule claim / CAS.
type ClaimAlertRuleStore interface {
	ClaimAlertRuleFire(ctx context.Context, id pgtype.UUID, minIntervalSecs int32) (bool, error)
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

	safego.Go(e.logger, "alerting-evaluator", func() {
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
	})

	return func() {
		cancel()
		e.wg.Wait()
	}
}

// EvaluateOnce executes an evaluation pass over all applications and their rules concurrently with bounded concurrency.
func (e *Evaluator) EvaluateOnce(ctx context.Context) error {
	apps, err := e.store.ListAllApplications(ctx)
	if err != nil {
		return fmt.Errorf("listing applications: %w", err)
	}

	maxWorkers := 16
	if len(apps) < maxWorkers {
		maxWorkers = len(apps)
	}
	if maxWorkers == 0 {
		return nil
	}

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

AppLoop:
	for _, app := range apps {
		if ctx.Err() != nil {
			break
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break AppLoop
		}

		wg.Add(1)
		go func(a gen.Application) {
			defer func() {
				<-sem
				wg.Done()
			}()
			e.evaluateApp(ctx, a)
		}(app)
	}
	wg.Wait()

	return ctx.Err()
}

func (e *Evaluator) evaluateApp(ctx context.Context, app gen.Application) {
	appCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rules, err := e.store.ListAlertingRulesByApplication(appCtx, app.ID)
	if err != nil {
		e.logger.Warn("failed to list rules for application", "app", app.Name, "error", err)
		return
	}

	for _, rule := range rules {
		if appCtx.Err() != nil {
			return
		}

		ruleCtx, ruleCancel := context.WithTimeout(appCtx, 5*time.Second)
		triggered, meta, err := e.evaluateRule(ruleCtx, app, rule)
		if err != nil {
			ruleCancel()
			e.logger.Warn("evaluating rule failed", "ruleID", rule.ID, "type", rule.RuleType, "error", err)
			continue
		}

		if triggered {
			e.fireAlert(ruleCtx, rule, meta)
		}
		ruleCancel()
	}
}

func (e *Evaluator) claimRule(ctx context.Context, rule gen.AlertingRule) (bool, error) {
	var minInterval int32
	if rule.MinIntervalSecs != nil && *rule.MinIntervalSecs > 0 {
		minInterval = *rule.MinIntervalSecs
	}

	if cs, ok := e.store.(ClaimAlertRuleStore); ok {
		return cs.ClaimAlertRuleFire(ctx, rule.ID, minInterval)
	}

	// Fallback when ClaimAlertRuleStore is not implemented
	if e.shouldThrottle(rule) {
		return false, nil
	}
	if err := e.store.TouchAlertRuleLastFired(ctx, rule.ID); err != nil {
		return false, err
	}
	return true, nil
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
		var metaMap map[string]any
		if len(rule.RuleMetadata) > 0 {
			_ = json.Unmarshal(rule.RuleMetadata, &metaMap)
		}
		threshold := 1
		if t, ok := metaMap["threshold"].(float64); ok && t > 0 {
			threshold = int(t)
		} else if s, ok := metaMap["threshold"].(string); ok {
			if v, err := strconv.Atoi(s); err == nil && v > 0 {
				threshold = v
			}
		}
		wfName := "*"
		if w, ok := metaMap["workflow_name"].(string); ok && w != "" {
			wfName = w
		}
		periodSecs := 60
		if p, ok := metaMap["period_secs"].(float64); ok && p > 0 {
			periodSecs = int(p)
		} else if s, ok := metaMap["period_secs"].(string); ok {
			if v, err := strconv.Atoi(s); err == nil && v > 0 {
				periodSecs = v
			}
		}

		if e.dispatcher == nil {
			return false, nil, nil
		}

		startTime := time.Now().UTC().Add(-time.Duration(periodSecs) * time.Second)
		req := &protocol.ListWorkflowsRequest{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListWorkflows,
				RequestID: uuid.New().String(),
			},
			Body: protocol.ListWorkflowsRequestBody{
				StartTime: &startTime,
				Status:    protocol.StringOrList{"ERROR"},
			},
		}
		if wfName != "*" && wfName != "" {
			req.Body.WorkflowName = protocol.StringOrList{wfName}
		}

		respMsg, err := e.dispatcher.Dispatch(ctx, app.ID, req)
		if err != nil {
			e.logger.Warn("evaluating workflow failure rule failed to query executor", "app", app.Name, "error", err)
			return false, nil, nil
		}

		failedCount := 0
		if resp, ok := respMsg.(*protocol.ListWorkflowsResponse); ok {
			for _, wf := range resp.Output {
				if wfName != "*" && wfName != "" && wf.WorkflowName != nil && *wf.WorkflowName != wfName {
					continue
				}
				failedCount++
			}
		}

		if failedCount >= threshold {
			meta := map[string]string{
				"workflow_name":         wfName,
				"failed_workflow_count": strconv.Itoa(failedCount),
				"threshold":             strconv.Itoa(threshold),
				"period_secs":           strconv.Itoa(periodSecs),
			}
			return true, meta, nil
		}
		return false, nil, nil

	case "SlowQueue":
		var metaMap map[string]any
		if len(rule.RuleMetadata) > 0 {
			_ = json.Unmarshal(rule.RuleMetadata, &metaMap)
		}
		qName := "*"
		if q, ok := metaMap["queue_name"].(string); ok && q != "" {
			qName = q
		}
		threshSecs := 60
		if ts, ok := metaMap["threshold_secs"].(float64); ok && ts > 0 {
			threshSecs = int(ts)
		} else if s, ok := metaMap["threshold_secs"].(string); ok {
			if v, err := strconv.Atoi(s); err == nil && v > 0 {
				threshSecs = v
			}
		}

		if e.dispatcher == nil {
			return false, nil, nil
		}

		req := &protocol.ListWorkflowsRequest{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListQueuedWorkflows,
				RequestID: uuid.New().String(),
			},
			Body: protocol.ListWorkflowsRequestBody{
				QueuesOnly: true,
			},
		}
		if qName != "*" && qName != "" {
			req.Body.QueueName = protocol.StringOrList{qName}
		}

		respMsg, err := e.dispatcher.Dispatch(ctx, app.ID, req)
		if err != nil {
			e.logger.Warn("evaluating slow queue rule failed to query executor", "app", app.Name, "error", err)
			return false, nil, nil
		}

		stuckCount := 0
		cutoff := time.Now().UTC().Add(-time.Duration(threshSecs) * time.Second)
		if resp, ok := respMsg.(*protocol.ListWorkflowsResponse); ok {
			for _, wf := range resp.Output {
				if qName != "*" && qName != "" && wf.QueueName != nil && *wf.QueueName != qName {
					continue
				}
				if wf.CreatedAt != nil {
					var createdTime time.Time
					if ms, err := strconv.ParseInt(*wf.CreatedAt, 10, 64); err == nil {
						createdTime = time.UnixMilli(ms).UTC()
					} else if t, err := time.Parse(time.RFC3339, *wf.CreatedAt); err == nil {
						createdTime = t.UTC()
					}
					if !createdTime.IsZero() && createdTime.Before(cutoff) {
						stuckCount++
					}
				}
			}
		}

		if stuckCount > 0 {
			meta := map[string]string{
				"queue_name":           qName,
				"stuck_workflow_count": strconv.Itoa(stuckCount),
				"threshold_secs":       strconv.Itoa(threshSecs),
			}
			return true, meta, nil
		}
		return false, nil, nil

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
	claimed, err := e.claimRule(ctx, rule)
	if err != nil {
		e.logger.Warn("failed to claim alert rule fire", "ruleID", rule.ID, "error", err)
		return
	}
	if !claimed {
		return
	}

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
		dispatchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := e.dispatcher.Dispatch(dispatchCtx, rule.ReceivingApplicationID, alertReq)
		cancel()
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
						destCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
						if err := e.channelDispatch.Dispatch(destCtx, dest, notif); err != nil {
							sanitizedErr := strings.ReplaceAll(err.Error(), dest.URL, "[REDACTED]")
							e.logger.Warn("failed to dispatch to external channel", "type", dest.Type, "error", sanitizedErr)
						}
						cancel()
					}
				}
			}
		}
	}
}
