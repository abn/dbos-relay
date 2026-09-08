package alerting_test

import (
	"context"
	"sync"
	"testing"

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
	mu        sync.Mutex
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
