package alerting_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/alerting"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

type testStore struct {
	apps      []gen.Application
	rules     map[string][]gen.AlertingRule
	executors map[string][]gen.Executor
}

func (s *testStore) ListAllApplications(ctx context.Context) ([]gen.Application, error) {
	return s.apps, nil
}

func (s *testStore) ListAlertingRulesByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.AlertingRule, error) {
	idStr := uuid.UUID(appID.Bytes).String()
	return s.rules[idStr], nil
}

func (s *testStore) ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error) {
	idStr := uuid.UUID(appID.Bytes).String()
	return s.executors[idStr], nil
}

func (s *testStore) TouchAlertRuleLastFired(ctx context.Context, id pgtype.UUID) error {
	return nil
}

type testDispatcher struct{}

func (d *testDispatcher) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	return nil, nil
}

func TestAlertingChannels_EndToEnd(t *testing.T) {
	webhookSecret := "super-secret-key-123"
	webhookCh := make(chan struct {
		sig       string
		timestamp string
		body      []byte
	}, 1)

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		webhookCh <- struct {
			sig       string
			timestamp string
			body      []byte
		}{
			sig:       r.Header.Get("X-Relay-Signature"),
			timestamp: r.Header.Get("X-Relay-Timestamp"),
			body:      body,
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	slackCh := make(chan map[string]any, 1)
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		slackCh <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer slackServer.Close()

	pdCh := make(chan map[string]any, 1)
	pdServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		pdCh <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer pdServer.Close()

	appIDBytes := uuid.New()
	appID := pgtype.UUID{Bytes: appIDBytes, Valid: true}
	appIDStr := appIDBytes.String()

	ruleMeta := map[string]any{
		"destinations": []map[string]any{
			{
				"type":   "webhook",
				"url":    webhookServer.URL,
				"secret": webhookSecret,
			},
			{
				"type": "slack",
				"url":  slackServer.URL,
			},
			{
				"type":        "pagerduty",
				"url":         pdServer.URL,
				"routing_key": "pd-route-999",
			},
		},
	}
	ruleMetaBytes, _ := json.Marshal(ruleMeta)

	ruleID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	mockStore := &testStore{
		apps: []gen.Application{
			{ID: appID, Name: "test-app"},
		},
		rules: map[string][]gen.AlertingRule{
			appIDStr: {
				{
					ID:            ruleID,
					ApplicationID: appID,
					RuleType:      "UnresponsiveApplication",
					RuleMetadata:  ruleMetaBytes,
				},
			},
		},
		executors: map[string][]gen.Executor{
			appIDStr: {}, // 0 connected executors -> UnresponsiveApplication triggers
		},
	}

	evaluator := alerting.NewEvaluator(mockStore, &testDispatcher{}, nil)
	httpDispatcher := alerting.NewHTTPChannelDispatcher(nil)
	evaluator.SetChannelDispatcher(httpDispatcher)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce failed: %v", err)
	}

	// 1. Verify Webhook delivery
	select {
	case wh := <-webhookCh:
		if wh.timestamp == "" {
			t.Error("expected non-empty X-Relay-Timestamp header")
		}
		if !alerting.VerifyWebhookSignature(webhookSecret, wh.timestamp, wh.body, wh.sig) {
			t.Errorf("VerifyWebhookSignature failed: sig=%s, ts=%s", wh.sig, wh.timestamp)
		}
		var notif alerting.AlertNotification
		if err := json.Unmarshal(wh.body, &notif); err != nil {
			t.Fatalf("unmarshaling webhook notification: %v", err)
		}
		if notif.RuleType != "UnresponsiveApplication" {
			t.Errorf("expected RuleType UnresponsiveApplication, got %s", notif.RuleType)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for webhook dispatch")
	}

	// 2. Verify Slack delivery
	select {
	case sl := <-slackCh:
		text, ok := sl["text"].(string)
		if !ok || text == "" {
			t.Errorf("expected non-empty slack text field, got %v", sl)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for slack dispatch")
	}

	// 3. Verify PagerDuty delivery
	select {
	case pd := <-pdCh:
		rk, ok := pd["routing_key"].(string)
		if !ok || rk != "pd-route-999" {
			t.Errorf("expected routing_key pd-route-999, got %v", rk)
		}
		action, _ := pd["event_action"].(string)
		if action != "trigger" {
			t.Errorf("expected event_action trigger, got %v", action)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pagerduty dispatch")
	}
}
