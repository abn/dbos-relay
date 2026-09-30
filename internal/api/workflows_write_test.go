package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
)

func TestRewindWorkflowResult(t *testing.T) {
	t.Run("success returns 204", func(t *testing.T) {
		r := &mockRouter{dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
			return &protocol.RewindWorkflowResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeRewindWorkflow},
				Success:  true,
			}, nil
		}}
		srv := NewServer(r, nil, nil)
		resp, err := srv.RewindWorkflow(context.Background(), gen.RewindWorkflowRequestObject{
			OrgName:    "local",
			AppName:    "app",
			WorkflowId: "wf-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.RewindWorkflow204Response); !ok {
			t.Fatalf("expected RewindWorkflow204Response, got %T", resp)
		}
	})

	t.Run("false success returns 400", func(t *testing.T) {
		r := &mockRouter{dispatchFn: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
			return &protocol.RewindWorkflowResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeRewindWorkflow},
				Success:  false,
			}, nil
		}}
		srv := NewServer(r, nil, nil)
		resp, err := srv.RewindWorkflow(context.Background(), gen.RewindWorkflowRequestObject{
			OrgName:    "local",
			AppName:    "app",
			WorkflowId: "wf-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		prob, ok := resp.(gen.RewindWorkflowdefaultApplicationProblemPlusJSONResponse)
		if !ok || prob.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for false success, got %T", resp)
		}
	})
}
