package alerting_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/alerting"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

type mockAlertStore struct {
	mu           sync.Mutex
	rules        []gen.AlertingRule
	executors    map[pgtype.UUID][]gen.Executor
	touchedRules []pgtype.UUID
}

func (m *mockAlertStore) ListAllApplications(ctx context.Context) ([]gen.Application, error) {
	return []gen.Application{
		{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Name: "monitored-app"},
		{ID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}, Name: "receiving-app"},
	}, nil
}

func (m *mockAlertStore) ListAlertingRulesByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.AlertingRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res []gen.AlertingRule
	for _, r := range m.rules {
		if r.ApplicationID == appID {
			res = append(res, r)
		}
	}
	return res, nil
}

func (m *mockAlertStore) ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.executors[appID], nil
}

func (m *mockAlertStore) TouchAlertRuleLastFired(ctx context.Context, id pgtype.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touchedRules = append(m.touchedRules, id)
	return nil
}

type mockAlertDispatcher struct {
	mu         sync.Mutex
	dispatched []protocol.Message
}

func (m *mockAlertDispatcher) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatched = append(m.dispatched, msg)
	return &protocol.Envelope{Type: protocol.MessageTypeAlert, RequestID: msg.GetRequestID()}, nil
}

func TestAlertEvaluator_UnresponsiveApplication(t *testing.T) {
	monitoredAppID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	receivingAppID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ruleID := pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	interval := int32(60)

	store := &mockAlertStore{
		rules: []gen.AlertingRule{
			{
				ID:                     ruleID,
				ApplicationID:          monitoredAppID,
				ReceivingApplicationID: receivingAppID,
				RuleType:               "UnresponsiveApplication",
				RuleMetadata:           []byte(`{}`),
				MinIntervalSecs:        &interval,
			},
		},
		executors: map[pgtype.UUID][]gen.Executor{
			monitoredAppID: {}, // 0 connected executors -> unresponsive
			receivingAppID: {
				{
					ApplicationID: receivingAppID,
					ExecutorID:    "exec-recv-1",
					Status:        "connected",
				},
			},
		},
	}

	dispatcher := &mockAlertDispatcher{}
	evaluator := alerting.NewEvaluator(store, dispatcher, nil)

	ctx := context.Background()

	// Evaluate conditions
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce failed: %v", err)
	}

	dispatcher.mu.Lock()
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected 1 alert dispatched, got %d", len(dispatcher.dispatched))
	}
	alertReq, ok := dispatcher.dispatched[0].(*protocol.AlertRequest)
	if !ok {
		t.Fatalf("unexpected message type: %T", dispatcher.dispatched[0])
	}
	if alertReq.Name != "UnresponsiveApplication" {
		t.Errorf("expected alert name 'UnresponsiveApplication', got %q", alertReq.Name)
	}
	if alertReq.Metadata["application_name"] != "monitored-app" {
		t.Errorf("expected application_name 'monitored-app', got %q", alertReq.Metadata["application_name"])
	}
	dispatcher.mu.Unlock()

	store.mu.Lock()
	if len(store.touchedRules) != 1 || store.touchedRules[0] != ruleID {
		t.Errorf("expected rule %v to be touched, got %v", ruleID, store.touchedRules)
	}
	store.mu.Unlock()
}

type mockRecoveryAlertStore struct {
	mockAlertStore
	dispatches []gen.RecoveryDispatch
}

func (m *mockRecoveryAlertStore) ListRecentRecoveryDispatches(ctx context.Context, arg gen.ListRecentRecoveryDispatchesParams) ([]gen.RecoveryDispatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dispatches, nil
}

func TestAlertEvaluator_RecoveryFlapping(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	recvAppID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ruleID := pgtype.UUID{Bytes: [16]byte{10}, Valid: true}

	metaJSON := `{"threshold": 2}`
	store := &mockRecoveryAlertStore{
		mockAlertStore: mockAlertStore{
			rules: []gen.AlertingRule{
				{
					ID:                     ruleID,
					ApplicationID:          appID,
					ReceivingApplicationID: recvAppID,
					RuleType:               "RecoveryFlapping",
					RuleMetadata:           []byte(metaJSON),
				},
			},
			executors: map[pgtype.UUID][]gen.Executor{
				appID: {},
			},
		},
		dispatches: []gen.RecoveryDispatch{
			{DeadExecutorID: "exec-flapping-1"},
			{DeadExecutorID: "exec-flapping-1"},
		},
	}

	dispatcher := &mockAlertDispatcher{}
	evaluator := alerting.NewEvaluator(store, dispatcher, nil)

	ctx := context.Background()
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce failed: %v", err)
	}

	dispatcher.mu.Lock()
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected 1 alert dispatched, got %d", len(dispatcher.dispatched))
	}
	req := dispatcher.dispatched[0].(*protocol.AlertRequest)
	if req.Name != "RecoveryFlapping" {
		t.Errorf("expected RecoveryFlapping, got %s", req.Name)
	}
	if req.Metadata["flapping_executor_id"] != "exec-flapping-1" {
		t.Errorf("expected flapping_executor_id 'exec-flapping-1', got %s", req.Metadata["flapping_executor_id"])
	}
	dispatcher.mu.Unlock()
}

func TestAlertEvaluator_StrandedVersion(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	recvAppID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ruleID := pgtype.UUID{Bytes: [16]byte{11}, Valid: true}

	metaJSON := `{"stranded_version": "v2.1.0"}`
	store := &mockAlertStore{
		rules: []gen.AlertingRule{
			{
				ID:                     ruleID,
				ApplicationID:          appID,
				ReceivingApplicationID: recvAppID,
				RuleType:               "StrandedVersion",
				RuleMetadata:           []byte(metaJSON),
			},
		},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {
				{
					ApplicationID:      appID,
					ExecutorID:         "exec-1",
					Status:             "connected",
					ApplicationVersion: "v2.0.0", // different version
				},
			},
		},
	}

	dispatcher := &mockAlertDispatcher{}
	evaluator := alerting.NewEvaluator(store, dispatcher, nil)

	ctx := context.Background()
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce failed: %v", err)
	}

	dispatcher.mu.Lock()
	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected 1 alert for stranded version, got %d", len(dispatcher.dispatched))
	}
	req := dispatcher.dispatched[0].(*protocol.AlertRequest)
	if req.Metadata["stranded_version"] != "v2.1.0" {
		t.Errorf("expected stranded_version 'v2.1.0', got %s", req.Metadata["stranded_version"])
	}
	dispatcher.mu.Unlock()
}

type mockChannelDispatcher struct {
	mu            sync.Mutex
	notifications []alerting.AlertNotification
	destinations  []alerting.ChannelDestination
}

func (m *mockChannelDispatcher) Dispatch(ctx context.Context, dest alerting.ChannelDestination, notif alerting.AlertNotification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.destinations = append(m.destinations, dest)
	m.notifications = append(m.notifications, notif)
	return nil
}

func TestAlertEvaluator_ChannelDispatcherIntegration(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	recvAppID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ruleID := pgtype.UUID{Bytes: [16]byte{12}, Valid: true}

	metaJSON := `{
		"destinations": [
			{"type": "webhook", "url": "http://example.com/webhook", "secret": "sec123"},
			{"type": "slack", "url": "http://example.com/slack"}
		]
	}`
	store := &mockAlertStore{
		rules: []gen.AlertingRule{
			{
				ID:                     ruleID,
				ApplicationID:          appID,
				ReceivingApplicationID: recvAppID,
				RuleType:               "UnresponsiveApplication",
				RuleMetadata:           []byte(metaJSON),
			},
		},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {},
		},
	}

	dispatcher := &mockAlertDispatcher{}
	chanDispatcher := &mockChannelDispatcher{}
	evaluator := alerting.NewEvaluator(store, dispatcher, nil)
	evaluator.SetChannelDispatcher(chanDispatcher)

	ctx := context.Background()
	if err := evaluator.EvaluateOnce(ctx); err != nil {
		t.Fatalf("EvaluateOnce failed: %v", err)
	}

	chanDispatcher.mu.Lock()
	defer chanDispatcher.mu.Unlock()
	if len(chanDispatcher.notifications) != 2 {
		t.Fatalf("expected 2 channel notifications, got %d", len(chanDispatcher.notifications))
	}
	if chanDispatcher.destinations[0].Type != alerting.ChannelWebhook {
		t.Errorf("expected first dest to be webhook, got %s", chanDispatcher.destinations[0].Type)
	}
	if chanDispatcher.destinations[1].Type != alerting.ChannelSlack {
		t.Errorf("expected second dest to be slack, got %s", chanDispatcher.destinations[1].Type)
	}
}

func TestHTTPChannelDispatcher_Webhook(t *testing.T) {
	secret := "test-secret"
	received := false
	var receivedSig string
	var receivedTimestamp string
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		receivedSig = r.Header.Get("X-Relay-Signature")
		receivedTimestamp = r.Header.Get("X-Relay-Timestamp")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := alerting.NewHTTPChannelDispatcher(server.Client())
	notif := alerting.AlertNotification{
		RuleID:   "rule-1",
		RuleType: "UnresponsiveApplication",
		AppName:  "app-1",
		Message:  "alert triggered",
		FiredAt:  time.Now().UTC(),
	}

	dest := alerting.ChannelDestination{
		Type:   alerting.ChannelWebhook,
		URL:    server.URL,
		Secret: secret,
	}

	if err := dispatcher.Dispatch(context.Background(), dest, notif); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if !received {
		t.Fatal("expected server to receive request")
	}

	if receivedTimestamp == "" {
		t.Fatal("expected X-Relay-Timestamp header to be set")
	}

	// Verify constant-time HMAC-SHA256 signature against independent computation per docs/discovery/D8-metrics-alerting.md:286
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(receivedTimestamp + "."))
	mac.Write(receivedBody)
	expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if receivedSig != expectedSig {
		t.Errorf("independent signature mismatch: got %s, want %s", receivedSig, expectedSig)
	}
	if !alerting.VerifyWebhookSignature(secret, receivedTimestamp, receivedBody, receivedSig) {
		t.Errorf("VerifyWebhookSignature failed for sig %s, ts %s", receivedSig, receivedTimestamp)
	}
}

func TestHTTPChannelDispatcher_Slack(t *testing.T) {
	received := false
	var receivedText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		receivedText = body["text"]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := alerting.NewHTTPChannelDispatcher(server.Client())
	notif := alerting.AlertNotification{
		RuleID:   "rule-1",
		RuleType: "WorkflowFailure",
		AppName:  "my-app",
		Message:  "high failure rate",
		FiredAt:  time.Now().UTC(),
	}

	dest := alerting.ChannelDestination{
		Type: alerting.ChannelSlack,
		URL:  server.URL,
	}

	if err := dispatcher.Dispatch(context.Background(), dest, notif); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if !received {
		t.Fatal("expected server to receive request")
	}
	if receivedText == "" {
		t.Error("expected non-empty slack text")
	}
}

func TestHTTPChannelDispatcher_PagerDuty(t *testing.T) {
	received := false
	var receivedRoutingKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		receivedRoutingKey, _ = body["routing_key"].(string)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	dispatcher := alerting.NewHTTPChannelDispatcher(server.Client())
	notif := alerting.AlertNotification{
		RuleID:   "rule-1",
		RuleType: "SlowQueue",
		AppName:  "my-app",
		Message:  "queue backed up",
		FiredAt:  time.Now().UTC(),
	}

	dest := alerting.ChannelDestination{
		Type:       alerting.ChannelPagerDuty,
		URL:        server.URL,
		RoutingKey: "pd-key-xyz",
	}

	if err := dispatcher.Dispatch(context.Background(), dest, notif); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	if !received {
		t.Fatal("expected server to receive request")
	}
	if receivedRoutingKey != "pd-key-xyz" {
		t.Errorf("expected routing key pd-key-xyz, got %s", receivedRoutingKey)
	}
}

func TestHTTPChannelDispatcher_Errors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dispatcher := alerting.NewHTTPChannelDispatcher(server.Client())
	notif := alerting.AlertNotification{
		RuleID:   "rule-1",
		RuleType: "SlowQueue",
		AppName:  "my-app",
	}

	// 500 error from server
	dest := alerting.ChannelDestination{
		Type: alerting.ChannelWebhook,
		URL:  server.URL,
	}
	if err := dispatcher.Dispatch(context.Background(), dest, notif); err == nil {
		t.Fatal("expected error on 500 status code")
	}

	// Unsupported channel type
	unsupportedDest := alerting.ChannelDestination{
		Type: "unknown-chan",
		URL:  server.URL,
	}
	if err := dispatcher.Dispatch(context.Background(), unsupportedDest, notif); err == nil {
		t.Fatal("expected error on unsupported channel type")
	}
}
