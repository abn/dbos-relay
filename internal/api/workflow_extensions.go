package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/problem"
)

// Relay extension routes for workflow writes. These operations are not part of
// the vendored Conductor OpenAPI contract; their request and response shapes
// are documented in docs/usage/direct.md as Relay extensions.

const extensionBodyLimit = 1 << 20

type enqueueWorkflowRequest struct {
	WorkflowName       string          `json:"workflowName"`
	QueueName          string          `json:"queueName"`
	Payload            json.RawMessage `json:"payload"`
	WorkflowID         *string         `json:"workflowId,omitempty"`
	ApplicationVersion *string         `json:"appVersion,omitempty"`
	QueuePartitionKey  *string         `json:"queuePartitionKey,omitempty"`
}

type enqueueWorkflowResponse struct {
	WorkflowID string `json:"workflowId"`
}

type sendWorkflowMessageRequest struct {
	Topic          string          `json:"topic,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey *string         `json:"idempotencyKey,omitempty"`
}

// ServeEnqueueWorkflow handles POST .../workflows/enqueue.
func (s *Server) ServeEnqueueWorkflow(w http.ResponseWriter, r *http.Request) {
	var body enqueueWorkflowRequest
	if err := decodeExtensionBody(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	input := dataplane.EnqueueWorkflowInput{
		WorkflowName:       body.WorkflowName,
		QueueName:          body.QueueName,
		Payload:            body.Payload,
		WorkflowID:         body.WorkflowID,
		ApplicationVersion: body.ApplicationVersion,
		QueuePartitionKey:  body.QueuePartitionKey,
	}
	if err := input.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	router, ok := s.router.(interface {
		EnqueueWorkflow(ctx context.Context, orgName, appName string, input dataplane.EnqueueWorkflowInput) (dataplane.EnqueueWorkflowResult, error)
	})
	if !ok {
		writeProblem(w, http.StatusNotImplemented, "Not Implemented", "enqueue is not supported by this server")
		return
	}

	orgName := normalizeOrg(r.PathValue("orgName"))
	appName := r.PathValue("appName")
	res, err := router.EnqueueWorkflow(r.Context(), orgName, appName, input)
	if err != nil {
		writeRouterProblem(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/v2/orgs/%s/apps/%s/workflows/%s", orgName, appName, res.WorkflowID))
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(enqueueWorkflowResponse{WorkflowID: res.WorkflowID})
}

// ServeSendWorkflowMessage handles POST .../workflows/{workflowId}/send.
func (s *Server) ServeSendWorkflowMessage(w http.ResponseWriter, r *http.Request) {
	var body sendWorkflowMessageRequest
	if err := decodeExtensionBody(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	input := dataplane.SendWorkflowMessageInput{
		DestinationID:  r.PathValue("workflowId"),
		Topic:          body.Topic,
		Payload:        body.Payload,
		IdempotencyKey: body.IdempotencyKey,
	}
	if err := input.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	router, ok := s.router.(interface {
		SendWorkflowMessage(ctx context.Context, orgName, appName string, input dataplane.SendWorkflowMessageInput) error
	})
	if !ok {
		writeProblem(w, http.StatusNotImplemented, "Not Implemented", "send is not supported by this server")
		return
	}

	orgName := normalizeOrg(r.PathValue("orgName"))
	appName := r.PathValue("appName")
	if err := router.SendWorkflowMessage(r.Context(), orgName, appName, input); err != nil {
		writeRouterProblem(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func decodeExtensionBody(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is required")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, extensionBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if err == io.EOF {
			return fmt.Errorf("request body is required")
		}
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain a single JSON object")
	}
	return nil
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	problem.Write(w, &problem.Problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Detail: detail,
	})
}

func writeRouterProblem(w http.ResponseWriter, err error) {
	status, model := RouterErrorToModel(err)
	title := "Error"
	if model.Title != nil {
		title = *model.Title
	}
	detail := ""
	if model.Detail != nil {
		detail = *model.Detail
	}
	writeProblem(w, status, title, detail)
}
