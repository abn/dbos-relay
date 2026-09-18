package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/abn/relay/internal/problem"
	"github.com/abn/relay/internal/protocol"
)

// StreamEvent represents a server-sent event pushed to dashboard clients.
type StreamEvent struct {
	Type      string         `json:"type"`
	AppName   string         `json:"app_name,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// EventBroadcaster manages active SSE subscriptions and distributes events.
type EventBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan StreamEvent]struct{}
}

// NewEventBroadcaster creates a new event broadcaster instance.
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		subscribers: make(map[string]map[chan StreamEvent]struct{}),
	}
}

// Subscribe registers a listener for events associated with appName (or all apps if appName is empty).
func (b *EventBroadcaster) Subscribe(appName string) (<-chan StreamEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan StreamEvent, 16)
	if b.subscribers[appName] == nil {
		b.subscribers[appName] = make(map[chan StreamEvent]struct{})
	}
	b.subscribers[appName][ch] = struct{}{}

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if subs, ok := b.subscribers[appName]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(b.subscribers, appName)
			}
		}
		close(ch)
	}

	return ch, cancel
}

// Publish broadcasts an event to matching subscribers and fleet-wide listeners.
func (b *EventBroadcaster) Publish(evt StreamEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}

	// Deliver to app-specific subscribers
	if evt.AppName != "" {
		if subs, ok := b.subscribers[evt.AppName]; ok {
			for ch := range subs {
				select {
				case ch <- evt:
				default:
					// Drop if channel buffer is full to prevent slow consumers blocking publisher
				}
			}
		}
	}

	// Deliver to fleet-wide (empty appName) subscribers
	if subs, ok := b.subscribers[""]; ok {
		for ch := range subs {
			select {
			case ch <- evt:
			default:
			}
		}
	}
}

// ServeEvents handles SSE connections for /v2/orgs/{orgName}/apps/{appName}/events and /v2/orgs/{orgName}/events.
func (s *Server) ServeEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		problem.Write(w, &problem.Problem{
			Title:  "Streaming Unsupported",
			Status: http.StatusNotImplemented,
			Detail: "Server-Sent Events streaming is not supported by the underlying transport",
		})
		return
	}

	orgName := r.PathValue("orgName")
	appName := r.PathValue("appName")
	if orgName == "" {
		orgName = "local"
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Send initial ready event
	readyPayload, _ := json.Marshal(map[string]any{
		"org_name":  orgName,
		"app_name":  appName,
		"status":    "connected",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
	if _, err := fmt.Fprintf(w, "event: ready\ndata: %s\n\n", readyPayload); err != nil {
		return
	}
	flusher.Flush()

	ch, cancel := s.broadcaster.Subscribe(appName)
	defer cancel()

	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	// Periodic change detector for application workflows
	sweepTicker := time.NewTicker(2 * time.Second)
	defer sweepTicker.Stop()

	var lastWorkflowID string
	var lastStatus string

	for {
		select {
		case <-r.Context().Done():
			return

		case evt, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			eventName := evt.Type
			if eventName == "" {
				eventName = "message"
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, payload); err != nil {
				return
			}
			flusher.Flush()

		case t := <-pingTicker.C:
			if _, err := fmt.Fprintf(w, "event: ping\ndata: {\"timestamp\":%q}\n\n", t.UTC().Format(time.RFC3339)); err != nil {
				return
			}
			flusher.Flush()

		case <-sweepTicker.C:
			if appName == "" || s.router == nil {
				continue
			}

			// Poll the latest workflow to detect external additions or status updates
			limit := 1
			sortDesc := true
			req := &protocol.ListWorkflowsRequest{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeListWorkflows,
					RequestID: generateRequestID(),
				},
				Body: protocol.ListWorkflowsRequestBody{
					Limit:    &limit,
					SortDesc: sortDesc,
				},
			}

			checkCtx, checkCancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
			res, err := s.router.Dispatch(checkCtx, orgName, appName, req)
			checkCancel()

			if err != nil {
				continue
			}

			if listResp, ok := res.(*protocol.ListWorkflowsResponse); ok && len(listResp.Output) > 0 {
				wf := listResp.Output[0]
				wfStatus := ""
				if wf.Status != nil {
					wfStatus = *wf.Status
				}
				wfName := ""
				if wf.WorkflowName != nil {
					wfName = *wf.WorkflowName
				}

				if wf.WorkflowUUID != lastWorkflowID || wfStatus != lastStatus {
					lastWorkflowID = wf.WorkflowUUID
					lastStatus = wfStatus

					evt := StreamEvent{
						Type:    "workflow_update",
						AppName: appName,
						Data: map[string]any{
							"workflow_id": wf.WorkflowUUID,
							"name":        wfName,
							"status":      wfStatus,
						},
						Timestamp: time.Now().UTC(),
					}
					payload, _ := json.Marshal(evt)
					if _, err := fmt.Fprintf(w, "event: workflow_update\ndata: %s\n\n", payload); err != nil {
						return
					}
					flusher.Flush()
				}
			}
		}
	}
}
