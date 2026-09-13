package router_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	"github.com/abn/relay/internal/store/gen"
)

func TestForward_SignAndVerify(t *testing.T) {
	secret := []byte("test-cluster-secret-key")
	body := []byte(`{"type":"get_workflow","request_id":"req-1"}`)

	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8090/internal/v1/forward", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}

	now := time.Now()
	deadline := now.Add(5 * time.Second)

	router.SignRequest(req, body, secret, 0, deadline)

	// Verify headers were set
	if req.Header.Get(router.HeaderSignature) == "" {
		t.Fatal("expected signature header to be present")
	}
	if req.Header.Get(router.HeaderTimestamp) == "" {
		t.Fatal("expected timestamp header to be present")
	}
	if req.Header.Get(router.HeaderHop) != "0" {
		t.Fatalf("expected hop '0', got %q", req.Header.Get(router.HeaderHop))
	}
	if req.Header.Get(router.HeaderDeadline) == "" {
		t.Fatal("expected deadline header to be present")
	}

	// Verify valid request
	hop, extractedDeadline, err := router.VerifyRequest(req, body, secret, 30*time.Second)
	if err != nil {
		t.Fatalf("VerifyRequest failed: %v", err)
	}
	if hop != 0 {
		t.Errorf("expected hop 0, got %d", hop)
	}
	if extractedDeadline.Unix() != deadline.Unix() {
		t.Errorf("expected deadline %v, got %v", deadline.Unix(), extractedDeadline.Unix())
	}

	// Tampered body
	_, _, err = router.VerifyRequest(req, []byte(`tampered`), secret, 30*time.Second)
	if err == nil {
		t.Error("expected error for tampered body, got nil")
	}

	// Wrong secret
	_, _, err = router.VerifyRequest(req, body, []byte("wrong-secret"), 30*time.Second)
	if err == nil {
		t.Error("expected error for wrong secret, got nil")
	}

	// Excessive drift
	oldReq, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8090/internal/v1/forward", bytes.NewReader(body))
	router.SignRequest(oldReq, body, secret, 0, deadline)
	// Override timestamp to 1 hour ago
	oldReq.Header.Set(router.HeaderTimestamp, "100000")
	_, _, err = router.VerifyRequest(oldReq, body, secret, 30*time.Second)
	if err == nil {
		t.Error("expected error for expired timestamp drift, got nil")
	}

	// Loop prevention: hop >= 1 should fail verification
	loopReq, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:8090/internal/v1/forward", bytes.NewReader(body))
	router.SignRequest(loopReq, body, secret, 1, deadline)
	_, _, err = router.VerifyRequest(loopReq, body, secret, 30*time.Second)
	if err == nil {
		t.Error("expected loop prevention error for hop >= 1, got nil")
	}
}

func TestForward_RoundTrip(t *testing.T) {
	secret := []byte("test-cluster-secret")

	// Set up mock remote server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, _, err = router.VerifyRequest(r, body, secret, 30*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		msg, err := protocol.Decode(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Reply with a response message
		status := "SUCCESS"
		resp := &protocol.GetWorkflowResponse{
			Output: &protocol.ListWorkflowsResponseBody{
				WorkflowUUID: "wf-123",
				Status:       &status,
			},
		}
		resp.Type = protocol.MessageTypeGetWorkflow
		resp.RequestID = msg.GetRequestID()

		respBytes, _ := protocol.Encode(resp)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}))
	defer server.Close()

	client := router.NewForwarder(secret, nil)

	req := &protocol.GetWorkflowRequest{
		WorkflowID: "wf-123",
	}
	req.Type = protocol.MessageTypeGetWorkflow
	req.RequestID = "req-abc"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := client.Forward(ctx, server.URL+"/internal/v1/forward", req)
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}

	getWfRes, ok := res.(*protocol.GetWorkflowResponse)
	if !ok {
		t.Fatalf("unexpected response type: %T", res)
	}
	if getWfRes.Output == nil || getWfRes.Output.WorkflowUUID != "wf-123" || *getWfRes.Output.Status != "SUCCESS" {
		t.Errorf("unexpected response content: %+v", getWfRes)
	}
}

func TestForward_ZeroValueResponse(t *testing.T) {
	secret := []byte("test-cluster-secret")

	// Mock remote server returning a zero-value response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, _, err = router.VerifyRequest(r, body, secret, 30*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		msg, err := protocol.DecodeRequest(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Reply with bare zero-value response frame
		respBytes := []byte(fmt.Sprintf(`{"type":"get_workflow","request_id":"%s"}`, msg.GetRequestID()))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}))
	defer server.Close()

	client := router.NewForwarder(secret, nil)

	req := &protocol.GetWorkflowRequest{
		WorkflowID: "missing-wf",
	}
	req.Type = protocol.MessageTypeGetWorkflow
	req.RequestID = "req-zero-val"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := client.Forward(ctx, server.URL+"/internal/v1/forward", req)
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}

	getWfRes, ok := res.(*protocol.GetWorkflowResponse)
	if !ok {
		t.Fatalf("expected *protocol.GetWorkflowResponse, got %T", res)
	}
	if getWfRes.Output != nil {
		t.Errorf("expected nil Output, got %+v", getWfRes.Output)
	}
}

type mockInstanceStore struct {
	getOrgFunc      func(ctx context.Context, name string) (gen.Organisation, error)
	getAppFunc      func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error)
	listExecsFunc   func(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error)
	getInstanceFunc func(ctx context.Context, id pgtype.UUID) (gen.Instance, error)
}

func (m *mockInstanceStore) GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error) {
	if m.getOrgFunc != nil {
		return m.getOrgFunc(ctx, name)
	}
	return gen.Organisation{}, nil
}

func (m *mockInstanceStore) GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
	if m.getAppFunc != nil {
		return m.getAppFunc(ctx, arg)
	}
	return gen.Application{ID: arg.OrganisationID}, nil
}

func (m *mockInstanceStore) ListConnectedExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error) {
	if m.listExecsFunc != nil {
		return m.listExecsFunc(ctx, appID)
	}
	return nil, nil
}

func (m *mockInstanceStore) GetInstance(ctx context.Context, id pgtype.UUID) (gen.Instance, error) {
	if m.getInstanceFunc != nil {
		return m.getInstanceFunc(ctx, id)
	}
	return gen.Instance{}, nil
}

type mockForwarder struct {
	forwardFunc func(ctx context.Context, targetURL string, msg protocol.Message) (protocol.Message, error)
}

func (m *mockForwarder) Forward(ctx context.Context, targetURL string, msg protocol.Message) (protocol.Message, error) {
	if m.forwardFunc != nil {
		return m.forwardFunc(ctx, targetURL, msg)
	}
	return nil, nil
}

func TestRouter_PeerForwardingFallback(t *testing.T) {
	ctx := context.Background()
	localInstID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	remoteInstID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	appID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}

	store := &mockInstanceStore{
		getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
			return gen.Organisation{Name: name}, nil
		},
		getAppFunc: func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
			return gen.Application{ID: appID, Name: arg.Name}, nil
		},
		listExecsFunc: func(ctx context.Context, aID pgtype.UUID) ([]gen.Executor, error) {
			return []gen.Executor{
				{
					ExecutorID:      "remote-exec-1",
					OwnerInstanceID: remoteInstID,
				},
			}, nil
		},
		getInstanceFunc: func(ctx context.Context, id pgtype.UUID) (gen.Instance, error) {
			return gen.Instance{
				ID:               remoteInstID,
				AdvertiseAddress: "10.0.0.2",
				Port:             8090,
			}, nil
		},
	}

	// Hub has no local executor
	hub := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, aID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, router.ErrNoLiveExecutor
		},
	}

	forwardCalled := false
	forwarder := &mockForwarder{
		forwardFunc: func(ctx context.Context, targetURL string, msg protocol.Message) (protocol.Message, error) {
			forwardCalled = true
			expectedURL := "http://10.0.0.2:8090/internal/v1/forward/03000000-0000-0000-0000-000000000000"
			if targetURL != expectedURL {
				t.Errorf("expected target URL %q, got %q", expectedURL, targetURL)
			}
			return &protocol.Envelope{Type: protocol.MessageTypeListWorkflows}, nil
		},
	}

	r := router.New(store, hub)
	r.SetForwarder(forwarder, localInstID)

	msg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows}
	res, err := r.Dispatch(ctx, "acme", "test-app", msg)
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil response")
	}
	if !forwardCalled {
		t.Error("expected peer forwarder to have been invoked")
	}
}

func TestNewForwardHandler(t *testing.T) {
	secret := []byte("secret-key")
	appID := pgtype.UUID{Bytes: [16]byte{7}, Valid: true}

	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, aID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			if aID != appID {
				t.Errorf("expected appID %v, got %v", appID, aID)
			}
			return &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: msg.GetRequestID()}, nil
		},
	}

	handler := router.NewForwardHandler(dispatcher, secret, 30*time.Second)

	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "test-req"}
	body, _ := protocol.Encode(reqMsg)

	req, _ := http.NewRequest(http.MethodPost, "/internal/v1/forward/07000000-0000-0000-0000-000000000000", bytes.NewReader(body))
	router.SignRequest(req, body, secret, 0, time.Now().Add(5*time.Second))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	res, err := protocol.Decode(w.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.GetRequestID() != "test-req" {
		t.Errorf("expected request ID 'test-req', got %q", res.GetRequestID())
	}
}

func TestForward_EmptySecretRejection(t *testing.T) {
	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, aID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return &protocol.Envelope{Type: protocol.MessageTypeListWorkflows}, nil
		},
	}
	handler := router.NewForwardHandler(dispatcher, nil, 30*time.Second)

	body := []byte(`{"type":"list_workflows"}`)
	req, _ := http.NewRequest(http.MethodPost, "/internal/v1/forward/07000000-0000-0000-0000-000000000000", bytes.NewReader(body))
	router.SignRequest(req, body, nil, 0, time.Now().Add(5*time.Second))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for empty secret, got %d", w.Code)
	}
}

func TestForward_SignatureTamperingAndReplay(t *testing.T) {
	secret := []byte("fwd-secret-key")
	var dispatchCount int
	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, aID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			dispatchCount++
			return &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: msg.GetRequestID()}, nil
		},
	}
	handler := router.NewForwardHandler(dispatcher, secret, 30*time.Second)

	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "test-req"}
	body, _ := protocol.Encode(reqMsg)

	// 1. Modified deadline header -> 401
	{
		req, _ := http.NewRequest(http.MethodPost, "/internal/v1/forward/07000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		router.SignRequest(req, body, secret, 0, time.Now().Add(10*time.Second))
		req.Header.Set(router.HeaderDeadline, time.Now().Add(20*time.Second).Format(time.RFC3339Nano))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("modified deadline: expected 401, got %d", w.Code)
		}
	}

	// 2. Added query parameter -> 401
	{
		req, _ := http.NewRequest(http.MethodPost, "/internal/v1/forward/07000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		router.SignRequest(req, body, secret, 0, time.Now().Add(10*time.Second))
		// Tamper request URI by adding query param
		req.URL.RawQuery = "tampered=true"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("added query param: expected 401, got %d", w.Code)
		}
	}

	// 3. Replayed nonce -> 401
	{
		req1, _ := http.NewRequest(http.MethodPost, "/internal/v1/forward/07000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		router.SignRequest(req1, body, secret, 0, time.Now().Add(10*time.Second))
		w1 := httptest.NewRecorder()
		handler.ServeHTTP(w1, req1)
		if w1.Code != http.StatusOK {
			t.Fatalf("first presentation: expected 200, got %d", w1.Code)
		}

		// Replay exact same request
		req2, _ := http.NewRequest(http.MethodPost, "/internal/v1/forward/07000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		req2.Header = req1.Header.Clone()
		w2 := httptest.NewRecorder()
		handler.ServeHTTP(w2, req2)
		if w2.Code != http.StatusUnauthorized {
			t.Errorf("replayed nonce: expected 401, got %d", w2.Code)
		}
	}

	// 4. Expired deadline rejected before dispatch -> 401, dispatchCount unchanged
	{
		dispatchesBefore := dispatchCount
		req, _ := http.NewRequest(http.MethodPost, "/internal/v1/forward/07000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		pastDeadline := time.Now().Add(-10 * time.Second)
		router.SignRequest(req, body, secret, 0, pastDeadline)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expired deadline: expected 401, got %d", w.Code)
		}
		if dispatchCount != dispatchesBefore {
			t.Errorf("dispatcher called for expired deadline: dispatches before=%d, after=%d", dispatchesBefore, dispatchCount)
		}
	}
}

func TestForward_BodyCapAndCheapRejection(t *testing.T) {
	secret := []byte("fwd-secret-key")
	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, aID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return &protocol.Envelope{Type: protocol.MessageTypeListWorkflows}, nil
		},
	}
	handler := router.NewForwardHandler(dispatcher, secret, 30*time.Second)

	// 1. Unauthenticated request with large content -> 401 immediately without reading large body
	{
		largeBody := make([]byte, 64*1024*1024) // 64 MiB
		req, _ := http.NewRequest(http.MethodPost, "/internal/v1/forward/07000000-0000-0000-0000-000000000000", bytes.NewReader(largeBody))
		// No auth headers
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated probe, got %d", w.Code)
		}
	}

	// 2. Properly signed request exceeding 10 MiB limit -> 413 Payload Too Large
	{
		largeBody := make([]byte, 11*1024*1024) // 11 MiB
		req, _ := http.NewRequest(http.MethodPost, "/internal/v1/forward/07000000-0000-0000-0000-000000000000", bytes.NewReader(largeBody))
		router.SignRequest(req, largeBody, secret, 0, time.Now().Add(10*time.Second))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413 Request Entity Too Large, got %d: %s", w.Code, w.Body.String())
		}
	}
}
