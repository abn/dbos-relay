package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/metrics"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

type mockAggregateDispatcher struct {
	handler func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

func (m *mockAggregateDispatcher) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	return m.handler(ctx, appID, msg)
}

func strPtr(s string) *string { return &s }
func i64Ptr(v int64) *int64   { return &v }

func aggregateTestStore() *mockMetricsStore {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	return &mockMetricsStore{
		apps: []gen.Application{{ID: appID, Name: "test-app"}},
		executors: map[pgtype.UUID][]gen.Executor{
			appID: {{ApplicationID: appID, ExecutorID: "exec-1", Status: "connected", ApplicationVersion: "v1"}},
		},
	}
}

func cannedDispatcher() *mockAggregateDispatcher {
	return &mockAggregateDispatcher{
		handler: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			switch req := msg.(type) {
			case *protocol.GetStepAggregatesRequest:
				return &protocol.GetStepAggregatesResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeGetStepAggregates, RequestID: req.RequestID},
					Output: []protocol.StepAggregateRow{
						{Group: map[string]*string{"function_name": strPtr("charge"), "status": strPtr("SUCCESS")}, Count: i64Ptr(60), MaxDurationMs: i64Ptr(800)},
						{Group: map[string]*string{"function_name": strPtr("charge"), "status": strPtr("ERROR")}, Count: i64Ptr(12)},
					},
				}, nil
			case *protocol.GetWorkflowAggregatesRequest:
				b := req.Body
				if len(b.Status) > 0 && b.Status[0] == "ENQUEUED" {
					return &protocol.GetWorkflowAggregatesResponse{
						Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: req.RequestID},
						Output: []protocol.WorkflowAggregateRow{
							{Group: map[string]*string{"workflow_name": strPtr("orders"), "queue_name": strPtr("orders-q")}, Count: i64Ptr(5), MinCreatedAt: i64Ptr(1700000000000)},
						},
					}, nil
				}
				if len(b.Status) > 0 && b.Status[0] == "PENDING" {
					return &protocol.GetWorkflowAggregatesResponse{
						Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: req.RequestID},
						Output: []protocol.WorkflowAggregateRow{
							{Group: map[string]*string{"workflow_name": strPtr("orders")}, Count: i64Ptr(3), MinCreatedAt: i64Ptr(1700000060000)},
						},
					}, nil
				}
				if b.DequeuedAfter != nil {
					return &protocol.GetWorkflowAggregatesResponse{
						Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: req.RequestID},
						Output: []protocol.WorkflowAggregateRow{
							{Group: map[string]*string{"workflow_name": strPtr("orders"), "queue_name": strPtr("orders-q")}, Count: i64Ptr(90)},
						},
					}, nil
				}
				if b.CompletedAfter != nil {
					return &protocol.GetWorkflowAggregatesResponse{
						Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: req.RequestID},
						Output: []protocol.WorkflowAggregateRow{
							{Group: map[string]*string{"workflow_name": strPtr("orders"), "queue_name": strPtr("orders-q"), "status": strPtr("SUCCESS")}, Count: i64Ptr(60), MaxQueueWaitMs: i64Ptr(2000), MaxTotalLatencyMs: i64Ptr(5000)},
							{Group: map[string]*string{"workflow_name": strPtr("orders"), "queue_name": strPtr("orders-q"), "status": strPtr("ERROR")}, Count: i64Ptr(30)},
							{Group: map[string]*string{"workflow_name": strPtr("orders"), "queue_name": strPtr("orders-q"), "status": strPtr("CANCELLED")}, Count: i64Ptr(6)},
						},
					}, nil
				}
				return &protocol.GetWorkflowAggregatesResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: req.RequestID},
					Output: []protocol.WorkflowAggregateRow{
						{Group: map[string]*string{"workflow_name": strPtr("orders"), "queue_name": strPtr("orders-q")}, Count: i64Ptr(120)},
					},
				}, nil
			default:
				return nil, context.DeadlineExceeded
			}
		},
	}
}

func adminCtx(req *http.Request) *http.Request {
	ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{Subject: "admin", IsAdmin: true})
	return req.WithContext(ctx)
}

func TestMetricsAggregates_FullScrape(t *testing.T) {
	handler := metrics.NewHandlerWithDispatcher(aggregateTestStore(), cannedDispatcher())
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics", nil))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`dbos_conductor_v1_workflow_started_rate{application="test-app",workflow_name="orders",queue_name="orders-q"} 2 `,
		`dbos_conductor_v1_workflow_success_rate{application="test-app",workflow_name="orders",queue_name="orders-q"} 1 `,
		`dbos_conductor_v1_workflow_failed_rate{application="test-app",workflow_name="orders",queue_name="orders-q"} 0.5 `,
		`dbos_conductor_v1_workflow_cancelled_rate{application="test-app",workflow_name="orders",queue_name="orders-q"} 0.1 `,
		`dbos_conductor_v1_workflow_dequeued_rate{application="test-app",workflow_name="orders",queue_name="orders-q"} 1.5 `,
		`dbos_conductor_v1_workflow_enqueued_count{application="test-app",workflow_name="orders",queue_name="orders-q"} 5`,
		`dbos_conductor_v1_workflow_pending_count{application="test-app",workflow_name="orders"} 3`,
		`dbos_conductor_v1_workflow_oldest_enqueued_timestamp_seconds{application="test-app",workflow_name="orders",queue_name="orders-q"} 1700000000`,
		`dbos_conductor_v1_workflow_max_queue_wait_seconds{application="test-app",workflow_name="orders",queue_name="orders-q"} 2 `,
		`dbos_conductor_v1_workflow_max_total_latency_seconds{application="test-app",workflow_name="orders",queue_name="orders-q"} 5 `,
		`dbos_conductor_v1_step_success_rate{application="test-app",step_name="charge"} 1 `,
		`dbos_conductor_v1_step_failed_rate{application="test-app",step_name="charge"} 0.2 `,
		`dbos_conductor_v1_step_max_duration_seconds{application="test-app",step_name="charge"} 0.8 `,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected scrape to contain %q, got:\n%s", want, body)
		}
	}
}

func TestMetricsAggregates_MetricReadPermission(t *testing.T) {
	handler := metrics.NewHandlerWithDispatcher(aggregateTestStore(), cannedDispatcher())
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	ctx := auth.WithIdentity(req.Context(), &auth.UserIdentity{
		Subject:     "metrics-reader",
		IsAdmin:     false,
		IsAPIKey:    true,
		Permissions: []string{auth.PermMetricRead},
	})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for metric.read key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestMetricsAggregates_MetricsFilter(t *testing.T) {
	handler := metrics.NewHandlerWithDispatcher(aggregateTestStore(), cannedDispatcher())
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics?metrics=dbos_conductor_v1_executor_count", nil))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "dbos_conductor_v1_executor_count") {
		t.Errorf("expected executor family, got:\n%s", body)
	}
	if strings.Contains(body, "dbos_conductor_v1_workflow_started_rate") {
		t.Errorf("expected workflow families filtered out, got:\n%s", body)
	}
}

func TestMetricsAggregates_WorkflowNamesFilter(t *testing.T) {
	handler := metrics.NewHandlerWithDispatcher(aggregateTestStore(), cannedDispatcher())
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics?workflow_names=other", nil))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, `workflow_name="orders"`) {
		t.Errorf("expected workflow_names filter to exclude orders, got:\n%s", body)
	}
	if !strings.Contains(body, "dbos_conductor_v1_executor_count") {
		t.Errorf("expected executor counts unaffected by workflow_names filter, got:\n%s", body)
	}
	// Step series live in the step_name namespace and are intentionally
	// left unfiltered by workflow_names.
	if !strings.Contains(body, `dbos_conductor_v1_step_success_rate{application="test-app",step_name="charge"}`) {
		t.Errorf("expected step series unaffected by workflow_names filter, got:\n%s", body)
	}
}

func TestMetricsAggregates_DispatcherErrorDegrades(t *testing.T) {
	failing := &mockAggregateDispatcher{
		handler: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, context.DeadlineExceeded
		},
	}
	handler := metrics.NewHandlerWithDispatcher(aggregateTestStore(), failing)
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics", nil))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on dispatcher failure, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "dbos_conductor_v1_executor_count") {
		t.Errorf("expected executor counts still served, got:\n%s", body)
	}
	if strings.Contains(body, "dbos_conductor_v1_workflow_started_rate{") {
		t.Errorf("expected no workflow series on dispatcher failure, got:\n%s", body)
	}
}

type recordedRequest struct {
	msgType protocol.MessageType
	wfBody  *protocol.GetWorkflowAggregatesRequestBody
	stBody  *protocol.GetStepAggregatesRequestBody
}

type recordingDispatcher struct {
	requests []recordedRequest
}

func (m *recordingDispatcher) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	switch req := msg.(type) {
	case *protocol.GetWorkflowAggregatesRequest:
		body := req.Body
		m.requests = append(m.requests, recordedRequest{msgType: req.GetMessageType(), wfBody: &body})
		return &protocol.GetWorkflowAggregatesResponse{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: req.RequestID},
		}, nil
	case *protocol.GetStepAggregatesRequest:
		body := req.Body
		m.requests = append(m.requests, recordedRequest{msgType: req.GetMessageType(), stBody: &body})
		return &protocol.GetStepAggregatesResponse{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetStepAggregates, RequestID: req.RequestID},
		}, nil
	default:
		return nil, context.DeadlineExceeded
	}
}

func TestMetricsAggregates_RequestShape(t *testing.T) {
	rec := &recordingDispatcher{}
	handler := metrics.NewHandlerWithDispatcher(aggregateTestStore(), rec)
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics?workflow_names=orders", nil))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(rec.requests) != 6 {
		t.Fatalf("expected 6 aggregate dispatches, got %d", len(rec.requests))
	}

	var started, completed, dequeued, enqueued, pending *protocol.GetWorkflowAggregatesRequestBody
	var steps *protocol.GetStepAggregatesRequestBody
	for _, r := range rec.requests {
		if r.wfBody == nil {
			if steps != nil {
				t.Fatal("expected exactly one step aggregates request")
			}
			steps = r.stBody
			continue
		}
		b := r.wfBody
		switch {
		case len(b.Status) == 1 && b.Status[0] == "ENQUEUED":
			enqueued = b
		case len(b.Status) == 1 && b.Status[0] == "PENDING":
			pending = b
		case b.DequeuedAfter != nil:
			dequeued = b
		case b.CompletedAfter != nil:
			completed = b
		case b.StartTime != nil:
			started = b
		default:
			t.Fatalf("unexpected workflow aggregates request: %+v", b)
		}
	}
	for name, b := range map[string]*protocol.GetWorkflowAggregatesRequestBody{
		"started": started, "completed": completed, "dequeued": dequeued, "enqueued": enqueued, "pending": pending,
	} {
		if b == nil {
			t.Fatalf("missing %s aggregates request", name)
		}
	}

	// Window filters use the most recently completed clock-aligned minute.
	windowEnd := time.Now().UTC().Truncate(time.Minute)
	windowStart := windowEnd.Add(-time.Minute)
	checkWindow := func(label string, gotStart, gotEnd *time.Time) {
		t.Helper()
		if gotStart == nil || gotEnd == nil {
			t.Fatalf("%s request missing window", label)
		}
		if gotEnd.Sub(*gotStart) != time.Minute {
			t.Errorf("%s window is not one minute: %v to %v", label, gotStart, gotEnd)
		}
		if gotEnd.After(windowEnd.Add(time.Minute)) || gotEnd.Before(windowEnd.Add(-time.Minute)) {
			t.Errorf("%s window end %v far from expected %v", label, gotEnd, windowEnd)
		}
		if !gotStart.Equal(gotEnd.Add(-time.Minute)) {
			t.Errorf("%s window start %v is not one minute before end %v", label, gotStart, gotEnd)
		}
		_ = windowStart
	}
	checkWindow("started", started.StartTime, started.EndTime)
	checkWindow("completed", completed.CompletedAfter, completed.CompletedBefore)
	checkWindow("dequeued", dequeued.DequeuedAfter, dequeued.DequeuedBefore)
	checkWindow("steps", steps.CompletedAfter, steps.CompletedBefore)

	// Grouping and selection flags.
	if !started.GroupByName || !started.GroupByQueueName || !started.SelectCount {
		t.Errorf("started request missing grouping flags: %+v", started)
	}
	if !completed.GroupByStatus || !completed.GroupByName || !completed.GroupByQueueName ||
		!completed.SelectCount || !completed.SelectMaxQueueWaitMs || !completed.SelectMaxTotalLatencyMs {
		t.Errorf("completed request missing grouping flags: %+v", completed)
	}
	if !enqueued.GroupByName || !enqueued.GroupByQueueName || !enqueued.SelectCount || !enqueued.SelectMinCreatedAt {
		t.Errorf("enqueued request missing grouping flags: %+v", enqueued)
	}
	if !pending.GroupByName || !pending.SelectCount || !pending.SelectMinCreatedAt {
		t.Errorf("pending request missing grouping flags: %+v", pending)
	}
	if pending.GroupByQueueName {
		t.Errorf("pending request must not group by queue: %+v", pending)
	}
	if !steps.GroupByFunctionName || !steps.GroupByStatus || !steps.SelectCount || !steps.SelectMaxDurationMs {
		t.Errorf("steps request missing grouping flags: %+v", steps)
	}

	// workflow_names passes through to workflow Name filters but never to steps.
	for name, b := range map[string]*protocol.GetWorkflowAggregatesRequestBody{
		"started": started, "completed": completed, "dequeued": dequeued, "enqueued": enqueued, "pending": pending,
	} {
		if len(b.Name) != 1 || b.Name[0] != "orders" {
			t.Errorf("%s request Name filter = %v, want [orders]", name, b.Name)
		}
	}
	if len(steps.FunctionName) != 0 {
		t.Errorf("steps request must not carry workflow_names as FunctionName, got %v", steps.FunctionName)
	}
}

func TestMetricsAggregates_TimestampUnits(t *testing.T) {
	scrape := func(accept string) string {
		handler := metrics.NewHandlerWithDispatcher(aggregateTestStore(), cannedDispatcher())
		req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics", nil))
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		return w.Body.String()
	}

	findSeriesTS := func(body, prefix string) string {
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, prefix+"{") {
				fields := strings.Fields(line)
				if len(fields) != 3 {
					t.Fatalf("expected timestamped series for %s, got %q", prefix, line)
				}
				return fields[2]
			}
		}
		t.Fatalf("no series found for %s", prefix)
		return ""
	}

	// Prometheus text exposition stamps windowed series in milliseconds.
	plain := scrape("text/plain")
	ms := findSeriesTS(plain, "dbos_conductor_v1_workflow_success_rate")
	if len(ms) < 13 {
		t.Errorf("expected millisecond timestamp, got %q", ms)
	}
	if strings.Contains(plain, "# EOF") {
		t.Errorf("text/plain exposition must not contain # EOF")
	}

	// OpenMetrics stamps windowed series in seconds and ends with # EOF.
	om := scrape("application/openmetrics-text")
	sec := findSeriesTS(om, "dbos_conductor_v1_workflow_success_rate")
	if len(sec) != 10 {
		t.Errorf("expected second timestamp, got %q", sec)
	}
	if !strings.HasSuffix(strings.TrimSpace(om), "# EOF") {
		t.Errorf("openmetrics exposition must end with # EOF")
	}

	// Point-in-time series carry no timestamp in either format.
	for _, body := range []string{plain, om} {
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "dbos_conductor_v1_workflow_enqueued_count{") && len(strings.Fields(line)) != 2 {
				t.Errorf("point-in-time series must not carry a timestamp, got %q", line)
			}
		}
	}
}

func TestMetricsAggregates_ApplicationsFilter(t *testing.T) {
	appA := pgtype.UUID{Bytes: [16]byte{0xA}, Valid: true}
	appB := pgtype.UUID{Bytes: [16]byte{0xB}, Valid: true}
	store := &mockMetricsStore{
		apps: []gen.Application{
			{ID: appA, Name: "app-a"},
			{ID: appB, Name: "app-b"},
		},
		executors: map[pgtype.UUID][]gen.Executor{
			appA: {{ApplicationID: appA, ExecutorID: "exec-a", Status: "connected", ApplicationVersion: "v1"}},
			appB: {{ApplicationID: appB, ExecutorID: "exec-b", Status: "connected", ApplicationVersion: "v1"}},
		},
	}
	seen := make(map[string]bool)
	rec := &mockAggregateDispatcher{
		handler: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			switch req := msg.(type) {
			case *protocol.GetWorkflowAggregatesRequest:
				seen[appID.String()] = true
				return &protocol.GetWorkflowAggregatesResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: req.RequestID},
				}, nil
			case *protocol.GetStepAggregatesRequest:
				return &protocol.GetStepAggregatesResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeGetStepAggregates, RequestID: req.RequestID},
				}, nil
			default:
				return nil, context.DeadlineExceeded
			}
		},
	}
	handler := metrics.NewHandlerWithDispatcher(store, rec)
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/v1/metrics?applications=app-a", nil))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(seen) != 1 || !seen[appA.String()] {
		t.Errorf("expected dispatch only for app-a, got %v", seen)
	}
	body := w.Body.String()
	if strings.Contains(body, `application="app-b"`) {
		t.Errorf("expected no app-b series, got:\n%s", body)
	}
}
