package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
)

type extensionMockRouter struct {
	enqueue func(ctx context.Context, orgName, appName string, input dataplane.EnqueueWorkflowInput) (dataplane.EnqueueWorkflowResult, error)
	send    func(ctx context.Context, orgName, appName string, input dataplane.SendWorkflowMessageInput) error
}

func (m *extensionMockRouter) Dispatch(context.Context, string, string, protocol.Message) (protocol.Message, error) {
	return nil, nil
}

func (m *extensionMockRouter) EnqueueWorkflow(ctx context.Context, orgName, appName string, input dataplane.EnqueueWorkflowInput) (dataplane.EnqueueWorkflowResult, error) {
	if m.enqueue != nil {
		return m.enqueue(ctx, orgName, appName, input)
	}
	return dataplane.EnqueueWorkflowResult{}, nil
}

func (m *extensionMockRouter) SendWorkflowMessage(ctx context.Context, orgName, appName string, input dataplane.SendWorkflowMessageInput) error {
	if m.send != nil {
		return m.send(ctx, orgName, appName, input)
	}
	return nil
}

// stubStore is enough for NewHandler in no-auth mode; the extension handlers
// do not touch the store.
type stubStore struct{}

func (stubStore) Ping(context.Context) error { return nil }

func newExtensionHandler(m *extensionMockRouter) http.Handler {
	srv := NewServer(m, nil, nil)
	return NewHandler(stubStore{}, srv)
}

func TestEnqueueWorkflowExtension(t *testing.T) {
	var gotOrg, gotApp string
	var gotInput dataplane.EnqueueWorkflowInput
	m := &extensionMockRouter{
		enqueue: func(ctx context.Context, orgName, appName string, input dataplane.EnqueueWorkflowInput) (dataplane.EnqueueWorkflowResult, error) {
			gotOrg, gotApp, gotInput = orgName, appName, input
			return dataplane.EnqueueWorkflowResult{WorkflowID: "wf-new"}, nil
		},
	}
	handler := newExtensionHandler(m)

	body := `{"workflowName":"orders","queueName":"orders-q","payload":{"order":42}}`
	req := httptest.NewRequest(http.MethodPost, "/v2/orgs/acme/apps/shop/workflows/enqueue", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotOrg != "acme" || gotApp != "shop" {
		t.Fatalf("expected org acme app shop, got org=%q app=%q", gotOrg, gotApp)
	}
	if gotInput.WorkflowName != "orders" || gotInput.QueueName != "orders-q" {
		t.Fatalf("unexpected input: %+v", gotInput)
	}
	if string(gotInput.Payload) != `{"order":42}` {
		t.Fatalf("payload changed: %s", gotInput.Payload)
	}
	if loc := rec.Header().Get("Location"); loc != "/v2/orgs/acme/apps/shop/workflows/wf-new" {
		t.Fatalf("unexpected Location: %q", loc)
	}
	var out enqueueWorkflowResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.WorkflowID != "wf-new" {
		t.Fatalf("expected wf-new, got %q", out.WorkflowID)
	}
}

func TestSendWorkflowMessageExtension(t *testing.T) {
	var gotInput dataplane.SendWorkflowMessageInput
	m := &extensionMockRouter{
		send: func(ctx context.Context, orgName, appName string, input dataplane.SendWorkflowMessageInput) error {
			gotInput = input
			return nil
		},
	}
	handler := newExtensionHandler(m)

	body := `{"topic":"orders","payload":["ready",true]}`
	req := httptest.NewRequest(http.MethodPost, "/v2/orgs/acme/apps/shop/workflows/wf-1/send", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotInput.DestinationID != "wf-1" || gotInput.Topic != "orders" {
		t.Fatalf("unexpected input: %+v", gotInput)
	}
	if string(gotInput.Payload) != `["ready",true]` {
		t.Fatalf("payload changed: %s", gotInput.Payload)
	}
}

func TestWorkflowExtensionsErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		mock       *extensionMockRouter
		wantStatus int
		wantTitle  string
	}{
		{
			name: "enqueue read-only",
			path: "/v2/orgs/acme/apps/shop/workflows/enqueue",
			body: `{"workflowName":"orders","queueName":"orders-q","payload":{}}`,
			mock: &extensionMockRouter{
				enqueue: func(context.Context, string, string, dataplane.EnqueueWorkflowInput) (dataplane.EnqueueWorkflowResult, error) {
					return dataplane.EnqueueWorkflowResult{}, router.ErrReadOnlyMode
				},
			},
			wantStatus: http.StatusForbidden,
			wantTitle:  "Forbidden",
		},
		{
			name: "send missing data plane",
			path: "/v2/orgs/acme/apps/shop/workflows/wf-1/send",
			body: `{"payload":"hello"}`,
			mock: &extensionMockRouter{
				send: func(context.Context, string, string, dataplane.SendWorkflowMessageInput) error {
					return router.ErrOperationUnsupported
				},
			},
			wantStatus: http.StatusNotImplemented,
			wantTitle:  "Not Implemented",
		},
		{
			name:       "enqueue missing payload",
			path:       "/v2/orgs/acme/apps/shop/workflows/enqueue",
			body:       `{"workflowName":"orders","queueName":"orders-q"}`,
			mock:       &extensionMockRouter{},
			wantStatus: http.StatusBadRequest,
			wantTitle:  "Bad Request",
		},
		{
			name:       "send invalid JSON",
			path:       "/v2/orgs/acme/apps/shop/workflows/wf-1/send",
			body:       `{"payload":not-json}`,
			mock:       &extensionMockRouter{},
			wantStatus: http.StatusBadRequest,
			wantTitle:  "Bad Request",
		},
		{
			name:       "unknown field",
			path:       "/v2/orgs/acme/apps/shop/workflows/enqueue",
			body:       `{"workflowName":"orders","queueName":"orders-q","payload":{},"extra":1}`,
			mock:       &extensionMockRouter{},
			wantStatus: http.StatusBadRequest,
			wantTitle:  "Bad Request",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := newExtensionHandler(tc.mock)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("expected problem+json, got %q", ct)
			}
			var prob struct {
				Title  string `json:"title"`
				Status int    `json:"status"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if prob.Status != tc.wantStatus || prob.Title != tc.wantTitle {
				t.Fatalf("unexpected problem: %+v", prob)
			}
		})
	}
}

func TestWorkflowExtensionsRejectEmptyBody(t *testing.T) {
	handler := newExtensionHandler(&extensionMockRouter{})
	req := httptest.NewRequest(http.MethodPost, "/v2/orgs/acme/apps/shop/workflows/enqueue", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request body is required") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}
