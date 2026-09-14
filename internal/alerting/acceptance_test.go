package alerting_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/alerting"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

type mockQueryDispatcher struct {
	mu            sync.Mutex
	workflowsResp *protocol.ListWorkflowsResponse
	queuedResp    *protocol.ListWorkflowsResponse
	dispatched    []protocol.Message
	dispatchHook  func(ctx context.Context, appID pgtype.UUID, msg protocol.Message)
}

func (m *mockQueryDispatcher) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	if m.dispatchHook != nil {
		m.dispatchHook(ctx, appID, msg)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	switch msg.GetMessageType() {
	case protocol.MessageTypeListWorkflows:
		if m.workflowsResp != nil {
			return m.workflowsResp, nil
		}
		return &protocol.ListWorkflowsResponse{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: msg.GetRequestID()},
		}, nil

	case protocol.MessageTypeListQueuedWorkflows:
		if m.queuedResp != nil {
			return m.queuedResp, nil
		}
		return &protocol.ListWorkflowsResponse{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeListQueuedWorkflows, RequestID: msg.GetRequestID()},
		}, nil

	case protocol.MessageTypeAlert:
		m.dispatched = append(m.dispatched, msg)
		return &protocol.Envelope{Type: protocol.MessageTypeAlert, RequestID: msg.GetRequestID()}, nil

	default:
		return &protocol.Envelope{Type: msg.GetMessageType(), RequestID: msg.GetRequestID()}, nil
	}
}

func TestAlertEvaluator_WorkflowFailureFiresOnThreshold(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	recvAppID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ruleID := pgtype.UUID{Bytes: [16]byte{10}, Valid: true}

	store := &mockAlertStore{
		rules: []gen.AlertingRule{
			{
				ID:                     ruleID,
				ApplicationID:          appID,
				ReceivingApplicationID: recvAppID,
				RuleType:               "WorkflowFailure",
				RuleMetadata:           []byte(`{"workflow_name":"testWorkflow","threshold":"2","period_secs":"60"}`),
			},
		},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {
				{ExecutorID: "exec-1", Status: "connected", ApplicationVersion: "1.0"},
			},
		},
	}

	wfName := "testWorkflow"
	statusError := "ERROR"
	dispatcher := &mockQueryDispatcher{
		workflowsResp: &protocol.ListWorkflowsResponse{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows},
			Output: []protocol.ListWorkflowsResponseBody{
				{WorkflowUUID: "wf-1", WorkflowName: &wfName, Status: &statusError},
				{WorkflowUUID: "wf-2", WorkflowName: &wfName, Status: &statusError},
			},
		},
	}

	evaluator := alerting.NewEvaluator(store, dispatcher, nil)

	err := evaluator.EvaluateOnce(context.Background())
	if err != nil {
		t.Fatalf("EvaluateOnce failed: %v", err)
	}

	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()

	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected 1 alert dispatched, got %d", len(dispatcher.dispatched))
	}

	alertReq, ok := dispatcher.dispatched[0].(*protocol.AlertRequest)
	if !ok {
		t.Fatalf("dispatched message is not AlertRequest: %T", dispatcher.dispatched[0])
	}
	if alertReq.Name != "WorkflowFailure" {
		t.Errorf("expected alert name WorkflowFailure, got %s", alertReq.Name)
	}
	if alertReq.Metadata["failed_workflow_count"] != "2" {
		t.Errorf("expected failed_workflow_count=2, got %s", alertReq.Metadata["failed_workflow_count"])
	}
	if alertReq.Metadata["threshold"] != "2" {
		t.Errorf("expected threshold=2, got %s", alertReq.Metadata["threshold"])
	}
	if alertReq.Metadata["workflow_name"] != "testWorkflow" {
		t.Errorf("expected workflow_name=testWorkflow, got %s", alertReq.Metadata["workflow_name"])
	}
}

func TestAlertEvaluator_SlowQueueFiresOnThreshold(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	recvAppID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ruleID := pgtype.UUID{Bytes: [16]byte{11}, Valid: true}

	store := &mockAlertStore{
		rules: []gen.AlertingRule{
			{
				ID:                     ruleID,
				ApplicationID:          appID,
				ReceivingApplicationID: recvAppID,
				RuleType:               "SlowQueue",
				RuleMetadata:           []byte(`{"queue_name":"payments","threshold_secs":"30"}`),
			},
		},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {
				{ExecutorID: "exec-1", Status: "connected", ApplicationVersion: "1.0"},
			},
		},
	}

	qName := "payments"
	stuckCreatedAt := strconv.FormatInt(time.Now().Add(-60*time.Second).UnixMilli(), 10)
	dispatcher := &mockQueryDispatcher{
		queuedResp: &protocol.ListWorkflowsResponse{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeListQueuedWorkflows},
			Output: []protocol.ListWorkflowsResponseBody{
				{WorkflowUUID: "wf-stuck", QueueName: &qName, CreatedAt: &stuckCreatedAt},
			},
		},
	}

	evaluator := alerting.NewEvaluator(store, dispatcher, nil)

	err := evaluator.EvaluateOnce(context.Background())
	if err != nil {
		t.Fatalf("EvaluateOnce failed: %v", err)
	}

	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()

	if len(dispatcher.dispatched) != 1 {
		t.Fatalf("expected 1 alert dispatched, got %d", len(dispatcher.dispatched))
	}

	alertReq, ok := dispatcher.dispatched[0].(*protocol.AlertRequest)
	if !ok {
		t.Fatalf("dispatched message is not AlertRequest: %T", dispatcher.dispatched[0])
	}
	if alertReq.Name != "SlowQueue" {
		t.Errorf("expected alert name SlowQueue, got %s", alertReq.Name)
	}
	if alertReq.Metadata["stuck_workflow_count"] != "1" {
		t.Errorf("expected stuck_workflow_count=1, got %s", alertReq.Metadata["stuck_workflow_count"])
	}
	if alertReq.Metadata["queue_name"] != "payments" {
		t.Errorf("expected queue_name=payments, got %s", alertReq.Metadata["queue_name"])
	}
}

type atomicClaimStore struct {
	mockAlertStore
	claimCount atomic.Int32
	lastFired  sync.Map
}

func (s *atomicClaimStore) ClaimAlertRuleFire(ctx context.Context, id pgtype.UUID, minIntervalSecs int32) (bool, error) {
	key := id.String()
	now := time.Now()

	val, loaded := s.lastFired.Load(key)
	if loaded {
		prev := val.(time.Time)
		if minIntervalSecs > 0 && now.Sub(prev) < time.Duration(minIntervalSecs)*time.Second {
			return false, nil
		}
	}

	actual, loaded := s.lastFired.LoadOrStore(key, now)
	if loaded {
		prev := actual.(time.Time)
		if minIntervalSecs > 0 && now.Sub(prev) < time.Duration(minIntervalSecs)*time.Second {
			return false, nil
		}
		s.lastFired.Store(key, now)
	}

	s.claimCount.Add(1)
	return true, nil
}

func TestAlertEvaluator_ConcurrentEvaluatorDeduplication(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	recvAppID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ruleID := pgtype.UUID{Bytes: [16]byte{12}, Valid: true}
	minInterval := int32(60)

	sharedStore := &atomicClaimStore{
		mockAlertStore: mockAlertStore{
			rules: []gen.AlertingRule{
				{
					ID:                     ruleID,
					ApplicationID:          appID,
					ReceivingApplicationID: recvAppID,
					RuleType:               "UnresponsiveApplication",
					MinIntervalSecs:        &minInterval,
				},
			},
			executors: map[pgtype.UUID][]gen.Executor{
				appID: {}, // triggers UnresponsiveApplication
			},
		},
	}

	var totalDispatches atomic.Int32
	dispatcherA := &mockQueryDispatcher{
		dispatchHook: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) {
			if msg.GetMessageType() == protocol.MessageTypeAlert {
				totalDispatches.Add(1)
			}
		},
	}
	dispatcherB := &mockQueryDispatcher{
		dispatchHook: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) {
			if msg.GetMessageType() == protocol.MessageTypeAlert {
				totalDispatches.Add(1)
			}
		},
	}

	evaluator1 := alerting.NewEvaluator(sharedStore, dispatcherA, nil)
	evaluator2 := alerting.NewEvaluator(sharedStore, dispatcherB, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = evaluator1.EvaluateOnce(context.Background())
	}()
	go func() {
		defer wg.Done()
		_ = evaluator2.EvaluateOnce(context.Background())
	}()
	wg.Wait()

	if dispatches := totalDispatches.Load(); dispatches != 1 {
		t.Fatalf("expected exactly 1 alert dispatch across concurrent evaluators, got %d", dispatches)
	}
	if claims := sharedStore.claimCount.Load(); claims != 1 {
		t.Fatalf("expected exactly 1 claim across concurrent evaluators, got %d", claims)
	}
}

func TestAlertEvaluator_HeadOfLineBlockingResilience(t *testing.T) {
	var apps []gen.Application
	var rules []gen.AlertingRule
	executorsMap := make(map[pgtype.UUID][]gen.Executor)

	for i := 1; i <= 10; i++ {
		id := pgtype.UUID{Bytes: [16]byte{byte(i)}, Valid: true}
		apps = append(apps, gen.Application{ID: id, Name: fmt.Sprintf("app-%d", i)})
		rules = append(rules, gen.AlertingRule{
			ID:                     pgtype.UUID{Bytes: [16]byte{byte(100 + i)}, Valid: true},
			ApplicationID:          id,
			ReceivingApplicationID: id,
			RuleType:               "UnresponsiveApplication",
		})
		executorsMap[id] = []gen.Executor{}
	}

	store := &mockAlertStore{
		apps:      apps,
		rules:     rules,
		executors: executorsMap,
	}

	var successfulDispatches atomic.Int32
	dispatcher := &mockQueryDispatcher{
		dispatchHook: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) {
			if msg.GetMessageType() == protocol.MessageTypeAlert {
				// App 1 blocks until its context timeout expires
				if appID.Bytes[0] == 1 {
					<-ctx.Done()
					return
				}
				successfulDispatches.Add(1)
			}
		},
	}

	evaluator := alerting.NewEvaluator(store, dispatcher, nil)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := evaluator.EvaluateOnce(ctx)
	elapsed := time.Since(start)

	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("EvaluateOnce encountered unexpected error: %v", err)
	}

	if elapsed > 5*time.Second {
		t.Fatalf("EvaluateOnce took %v, exceeding 5s limit despite concurrent evaluation", elapsed)
	}

	if count := successfulDispatches.Load(); count != 9 {
		t.Fatalf("expected 9 non-blocking application alerts dispatched, got %d", count)
	}
}

func TestAlertEvaluator_DispatchDeadlinePropagation(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	recvAppID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	ruleID := pgtype.UUID{Bytes: [16]byte{15}, Valid: true}

	store := &mockAlertStore{
		rules: []gen.AlertingRule{
			{
				ID:                     ruleID,
				ApplicationID:          appID,
				ReceivingApplicationID: recvAppID,
				RuleType:               "UnresponsiveApplication",
			},
		},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {},
		},
	}

	var deadlineFound atomic.Bool
	dispatcher := &mockQueryDispatcher{
		dispatchHook: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) {
			if msg.GetMessageType() == protocol.MessageTypeAlert {
				_, hasDeadline := ctx.Deadline()
				if hasDeadline {
					deadlineFound.Store(true)
				}
			}
		},
	}

	evaluator := alerting.NewEvaluator(store, dispatcher, nil)
	if err := evaluator.EvaluateOnce(context.Background()); err != nil {
		t.Fatalf("EvaluateOnce failed: %v", err)
	}

	if !deadlineFound.Load() {
		t.Fatal("expected dispatch context to carry an active deadline")
	}
}

func TestAlertEvaluator_GracefulShutdownPrompt(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	store := &mockAlertStore{
		rules: []gen.AlertingRule{
			{
				ID:                     pgtype.UUID{Bytes: [16]byte{16}, Valid: true},
				ApplicationID:          appID,
				ReceivingApplicationID: appID,
				RuleType:               "UnresponsiveApplication",
			},
		},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {},
		},
	}

	dispatchStarted := make(chan struct{})
	dispatcher := &mockQueryDispatcher{
		dispatchHook: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) {
			if msg.GetMessageType() == protocol.MessageTypeAlert {
				close(dispatchStarted)
				<-ctx.Done()
			}
		},
	}

	evaluator := alerting.NewEvaluator(store, dispatcher, nil)
	stop := evaluator.Start(context.Background(), 10*time.Millisecond)

	select {
	case <-dispatchStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for dispatch to start")
	}

	shutdownStart := time.Now()
	stop()
	shutdownElapsed := time.Since(shutdownStart)

	if shutdownElapsed > 1*time.Second {
		t.Fatalf("expected prompt shutdown under 1s, took %v", shutdownElapsed)
	}
}

func TestHTTPChannelDispatcher_SSRFProtection(t *testing.T) {
	dispatcher := alerting.NewHTTPChannelDispatcher(nil)
	notif := alerting.AlertNotification{
		RuleID:   "rule-1",
		RuleType: "UnresponsiveApplication",
		AppName:  "app-1",
		Message:  "alert triggered",
		FiredAt:  time.Now(),
	}

	// 1. Loopback addresses blocked by default
	loopbackDest := alerting.ChannelDestination{
		Type: alerting.ChannelWebhook,
		URL:  "http://127.0.0.1:8090/healthz",
	}
	if err := dispatcher.Dispatch(context.Background(), loopbackDest, notif); err == nil {
		t.Fatal("expected loopback destination to be blocked by SSRF protection")
	}

	// 2. Cloud metadata link-local address blocked
	metadataDest := alerting.ChannelDestination{
		Type: alerting.ChannelWebhook,
		URL:  "http://169.254.169.254/latest/meta-data/",
	}
	if err := dispatcher.Dispatch(context.Background(), metadataDest, notif); err == nil {
		t.Fatal("expected cloud metadata IP to be blocked by SSRF protection")
	}

	// 3. RFC1918 private IP blocked
	privateDest := alerting.ChannelDestination{
		Type: alerting.ChannelWebhook,
		URL:  "http://10.0.0.1:8080/webhook",
	}
	if err := dispatcher.Dispatch(context.Background(), privateDest, notif); err == nil {
		t.Fatal("expected private IP to be blocked by SSRF protection")
	}

	// 4. Non-HTTP scheme blocked
	schemeDest := alerting.ChannelDestination{
		Type: alerting.ChannelWebhook,
		URL:  "file:///etc/passwd",
	}
	if err := dispatcher.Dispatch(context.Background(), schemeDest, notif); err == nil {
		t.Fatal("expected file scheme to be rejected")
	}

	// 5. Redirect to loopback blocked
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer redirector.Close()

	redirectDest := alerting.ChannelDestination{
		Type: alerting.ChannelWebhook,
		URL:  redirector.URL,
	}
	// Even though redirector itself is on loopback, if loopback is blocked it fails immediately
	if err := dispatcher.Dispatch(context.Background(), redirectDest, notif); err == nil {
		t.Fatal("expected redirect to loopback to fail")
	}
}
