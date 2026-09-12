package fakeexecutor_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/abn/relay/internal/fakeexecutor"
	"github.com/abn/relay/internal/protocol"
)

func TestFakeExecutor_ConnectAndInfo(t *testing.T) {
	infoChan := make(chan *protocol.ExecutorInfoResponse, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/websocket/testapp/testkey") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("accept error: %v", err)
		}
		defer func() { _ = conn.Close(websocket.StatusInternalError, "") }()

		req := protocol.ExecutorInfoRequest{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeExecutorInfo,
				RequestID: "req-info-1",
			},
		}
		reqData, err := protocol.Encode(&req)
		if err != nil {
			t.Fatalf("encode error: %v", err)
		}
		if err := conn.Write(r.Context(), websocket.MessageText, reqData); err != nil {
			t.Fatalf("write error: %v", err)
		}

		typ, data, err := conn.Read(r.Context())
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		if typ != websocket.MessageText {
			t.Fatalf("expected text message")
		}

		var info protocol.ExecutorInfoResponse
		if err := json.Unmarshal(data, &info); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		infoChan <- &info
	}))
	defer srv.Close()

	opts := fakeexecutor.Options{
		URL:                srv.URL,
		AppName:            "testapp",
		ConductorKey:       "testkey",
		ExecutorID:         "test-exec",
		ApplicationVersion: "1.0",
	}

	fe := fakeexecutor.New(opts)
	err := fe.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect error: %v", err)
	}
	defer func() { _ = fe.Close() }()

	select {
	case info := <-infoChan:
		if info.ExecutorID != "test-exec" {
			t.Errorf("expected ExecutorID 'test-exec', got %q", info.ExecutorID)
		}
		if info.ApplicationVersion != "1.0" {
			t.Errorf("expected ApplicationVersion '1.0', got %q", info.ApplicationVersion)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for executor_info")
	}
}

func TestFakeExecutor_MessageRoundTrip(t *testing.T) {
	reqChan := make(chan *protocol.GetWorkflowResponse, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("accept error: %v", err)
		}
		defer func() { _ = conn.Close(websocket.StatusInternalError, "") }()

		reqInfo := protocol.ExecutorInfoRequest{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeExecutorInfo,
				RequestID: "req-info",
			},
		}
		reqInfoData, _ := protocol.Encode(&reqInfo)
		_ = conn.Write(r.Context(), websocket.MessageText, reqInfoData)

		// discard executor_info
		_, _, _ = conn.Read(r.Context())

		// send get_workflow request
		req := protocol.GetWorkflowRequest{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetWorkflow,
				RequestID: "req-123",
			},
			WorkflowID: "wf-1",
		}
		data, _ := protocol.Encode(&req)
		_ = conn.Write(r.Context(), websocket.MessageText, data)

		// read response
		typ, data, err := conn.Read(r.Context())
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		if typ != websocket.MessageText {
			t.Fatalf("expected text message")
		}

		var resp protocol.GetWorkflowResponse
		_ = json.Unmarshal(data, &resp)
		reqChan <- &resp
	}))
	defer srv.Close()

	fe := fakeexecutor.New(fakeexecutor.Options{
		URL:          srv.URL,
		AppName:      "testapp",
		ConductorKey: "testkey",
	})
	_ = fe.Connect(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = fe.Run(ctx) }()

	select {
	case resp := <-reqChan:
		if resp.RequestID != "req-123" {
			t.Errorf("expected request_id 'req-123', got %q", resp.RequestID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestFakeExecutor_FaultInjection_Malformed(t *testing.T) {
	errChan := make(chan error, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, nil)
		defer func() { _ = conn.Close(websocket.StatusInternalError, "") }()
		reqInfo := protocol.ExecutorInfoRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeExecutorInfo, RequestID: "req-info"},
		}
		reqInfoData, _ := protocol.Encode(&reqInfo)
		_ = conn.Write(r.Context(), websocket.MessageText, reqInfoData)
		_, _, _ = conn.Read(r.Context())

		// send request
		req := protocol.GetWorkflowRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: "req-1"},
		}
		data, _ := protocol.Encode(&req)
		_ = conn.Write(r.Context(), websocket.MessageText, data)

		_, respData, err := conn.Read(r.Context())
		if err != nil {
			errChan <- err
			return
		}
		if string(respData) != "{ malformed json }" {
			t.Errorf("expected '{ malformed json }', got %q", string(respData))
		}
		errChan <- nil
	}))
	defer srv.Close()

	fe := fakeexecutor.New(fakeexecutor.Options{
		URL:               srv.URL,
		AppName:           "a",
		ConductorKey:      "k",
		SendMalformedJSON: true,
	})
	_ = fe.Connect(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = fe.Run(ctx) }()

	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestFakeExecutor_FaultInjection_Delayed(t *testing.T) {
	respChan := make(chan bool, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, nil)
		defer func() { _ = conn.Close(websocket.StatusInternalError, "") }()
		reqInfo := protocol.ExecutorInfoRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeExecutorInfo, RequestID: "req-info"},
		}
		reqInfoData, _ := protocol.Encode(&reqInfo)
		_ = conn.Write(r.Context(), websocket.MessageText, reqInfoData)
		_, _, _ = conn.Read(r.Context())

		start := time.Now()
		req := protocol.GetWorkflowRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: "req-1"},
		}
		data, _ := protocol.Encode(&req)
		_ = conn.Write(r.Context(), websocket.MessageText, data)

		_, _, _ = conn.Read(r.Context())
		respChan <- time.Since(start) > 200*time.Millisecond
	}))
	defer srv.Close()

	fe := fakeexecutor.New(fakeexecutor.Options{
		URL:           srv.URL,
		AppName:       "a",
		ConductorKey:  "k",
		ResponseDelay: 250 * time.Millisecond,
	})
	_ = fe.Connect(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = fe.Run(ctx) }()

	select {
	case delayed := <-respChan:
		if !delayed {
			t.Errorf("response was not delayed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestFakeExecutor_FaultInjection_AbruptClosure(t *testing.T) {
	closedChan := make(chan bool, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := websocket.Accept(w, r, nil)
		defer func() { _ = conn.Close(websocket.StatusInternalError, "") }()
		reqInfo := protocol.ExecutorInfoRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeExecutorInfo, RequestID: "req-info"},
		}
		reqInfoData, _ := protocol.Encode(&reqInfo)
		_ = conn.Write(r.Context(), websocket.MessageText, reqInfoData)
		_, _, _ = conn.Read(r.Context())

		req := protocol.GetWorkflowRequest{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: "req-1"},
		}
		data, _ := protocol.Encode(&req)
		_ = conn.Write(r.Context(), websocket.MessageText, data)

		_, _, err := conn.Read(r.Context())
		closedChan <- (websocket.CloseStatus(err) == websocket.StatusNormalClosure)
	}))
	defer srv.Close()

	fe := fakeexecutor.New(fakeexecutor.Options{
		URL:             srv.URL,
		AppName:         "a",
		ConductorKey:    "k",
		CloseMidRequest: true,
	})
	_ = fe.Connect(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := fe.Run(ctx)
	if err != nil {
		t.Errorf("unexpected error from run: %v", err)
	}

	select {
	case isNormalClosure := <-closedChan:
		if !isNormalClosure {
			t.Errorf("expected normal closure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
