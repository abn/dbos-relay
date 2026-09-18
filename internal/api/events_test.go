package api_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abn/relay/internal/api"
)

func TestEventBroadcaster(t *testing.T) {
	b := api.NewEventBroadcaster()

	// Subscribe to specific app
	appCh, cancelApp := b.Subscribe("test-app")
	defer cancelApp()

	// Subscribe fleet-wide
	fleetCh, cancelFleet := b.Subscribe("")
	defer cancelFleet()

	// Publish app-specific event
	b.Publish(api.StreamEvent{
		Type:    "workflow_update",
		AppName: "test-app",
		Data:    map[string]any{"workflow_id": "wf-1"},
	})

	select {
	case evt := <-appCh:
		if evt.Type != "workflow_update" || evt.AppName != "test-app" {
			t.Fatalf("unexpected app event: %+v", evt)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for app event")
	}

	select {
	case evt := <-fleetCh:
		if evt.Type != "workflow_update" {
			t.Fatalf("unexpected fleet event: %+v", evt)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for fleet event")
	}

	// Publish event for another app
	b.Publish(api.StreamEvent{
		Type:    "workflow_update",
		AppName: "other-app",
		Data:    map[string]any{"workflow_id": "wf-2"},
	})

	select {
	case evt := <-appCh:
		t.Fatalf("test-app subscriber should not receive other-app event: %+v", evt)
	case <-time.After(100 * time.Millisecond):
		// Expected: no event for test-app
	}

	select {
	case evt := <-fleetCh:
		if evt.AppName != "other-app" {
			t.Fatalf("fleet subscriber should receive other-app event: %+v", evt)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for fleet event on other-app")
	}
}

func TestServeEventsHTTP(t *testing.T) {
	server := api.NewServer(nil, nil, nil)
	handler := api.NewHandler(nil, server)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/v2/orgs/local/apps/test-app/events", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	w := httptest.NewRecorder()

	// Run handler in goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ServeHTTP(w, req)
	}()

	// Wait for handler to establish stream and write headers
	time.Sleep(100 * time.Millisecond)

	// Publish an event via the server's broadcaster
	server.PublishEvent(api.StreamEvent{
		Type:    "workflow_update",
		AppName: "test-app",
		Data:    map[string]any{"workflow_id": "wf-abc", "status": "SUCCESS"},
	})

	time.Sleep(100 * time.Millisecond)
	cancel() // Close client connection
	<-done

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("expected text/event-stream, got %s", contentType)
	}

	body := w.Body.String()
	scanner := bufio.NewScanner(strings.NewReader(body))
	var foundReady, foundUpdate bool

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ready") {
			foundReady = true
		}
		if strings.HasPrefix(line, "event: workflow_update") {
			foundUpdate = true
		}
	}

	if !foundReady {
		t.Fatalf("missing 'ready' event in SSE stream: %s", body)
	}
	if !foundUpdate {
		t.Fatalf("missing 'workflow_update' event in SSE stream: %s", body)
	}
}
