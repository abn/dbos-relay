package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
	relaystore "github.com/abn/relay/internal/store"
	storegen "github.com/abn/relay/internal/store/gen"
)

func resumeTestStore(t *testing.T, settings string, execs []storegen.Executor, audited *[]string) *mockStoreReader {
	t.Helper()
	orgID := pgtype.UUID{Bytes: [16]byte{7, 7, 7}, Valid: true}
	appID := pgtype.UUID{Bytes: [16]byte{8, 8, 8}, Valid: true}
	return &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: orgID, Name: name}, nil
		},
		getAppByNameFunc: func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
			return storegen.Application{ID: appID, OrganisationID: orgID, Name: "shop", Settings: []byte(settings)}, nil
		},
		listExecutorsByAppFunc: func(ctx context.Context, appID pgtype.UUID) ([]storegen.Executor, error) {
			return execs, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			*audited = append(*audited, arg.Action)
			return storegen.AuditLog{}, nil
		},
	}
}

func resumeTestRouter(dispatched *bool) *mockWorkflowRouter {
	return &mockWorkflowRouter{
		dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
			*dispatched = true
			return &protocol.ResumeWorkflowResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeResume, RequestID: msg.GetRequestID()},
				Success:  true,
			}, nil
		},
	}
}

func connectedExec(version string) storegen.Executor {
	return storegen.Executor{
		ExecutorID:         "exec-1",
		Status:             storegen.ExecutorStatusConnected,
		ApplicationVersion: version,
	}
}

// Resume requires a healthy executor on the application's latest version,
// not merely any healthy executor. A connected deployment still rolling
// out resumes nothing.
func TestResumeWorkflow_LatestVersionGuard(t *testing.T) {
	t.Run("proceeds when latest version has a healthy executor", func(t *testing.T) {
		var audited []string
		var dispatched bool
		store := resumeTestStore(t, `{"latestVersion":"v2"}`, []storegen.Executor{connectedExec("v1"), connectedExec("v2")}, &audited)
		srv := api.NewServer(resumeTestRouter(&dispatched), store, nil)

		resp, err := srv.ResumeWorkflow(context.Background(), gen.ResumeWorkflowRequestObject{
			OrgName: "acme", AppName: "shop", WorkflowId: "wf-1",
		})
		if err != nil {
			t.Fatalf("ResumeWorkflow error: %v", err)
		}
		if _, ok := resp.(gen.ResumeWorkflow204Response); !ok {
			t.Fatalf("expected 204, got %+v", resp)
		}
		if !dispatched {
			t.Error("expected dispatch to executor")
		}
	})

	t.Run("refused when latest version has no healthy executor", func(t *testing.T) {
		var audited []string
		var dispatched bool
		store := resumeTestStore(t, `{"latestVersion":"v2"}`, []storegen.Executor{connectedExec("v1")}, &audited)
		srv := api.NewServer(resumeTestRouter(&dispatched), store, nil)

		resp, err := srv.ResumeWorkflow(context.Background(), gen.ResumeWorkflowRequestObject{
			OrgName: "acme", AppName: "shop", WorkflowId: "wf-1",
		})
		if err != nil {
			t.Fatalf("ResumeWorkflow error: %v", err)
		}
		rej, ok := resp.(gen.ResumeWorkflowdefaultApplicationProblemPlusJSONResponse)
		if !ok || rej.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %+v", resp)
		}
		if dispatched {
			t.Error("dispatch must not run when the guard refuses")
		}
		if len(audited) != 1 || audited[0] != "workflow.resume" {
			t.Errorf("expected one workflow.resume failure entry, got %v", audited)
		}
	})

	t.Run("proceeds when no latest version is recorded", func(t *testing.T) {
		var audited []string
		var dispatched bool
		store := resumeTestStore(t, `{}`, []storegen.Executor{connectedExec("v1")}, &audited)
		srv := api.NewServer(resumeTestRouter(&dispatched), store, nil)

		resp, err := srv.ResumeWorkflow(context.Background(), gen.ResumeWorkflowRequestObject{
			OrgName: "acme", AppName: "shop", WorkflowId: "wf-1",
		})
		if err != nil {
			t.Fatalf("ResumeWorkflow error: %v", err)
		}
		if _, ok := resp.(gen.ResumeWorkflow204Response); !ok {
			t.Fatalf("expected 204, got %+v", resp)
		}
		if !dispatched {
			t.Error("expected dispatch to executor")
		}
	})

	t.Run("fail-open when the store cannot answer", func(t *testing.T) {
		var dispatched bool
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{}, errors.New("store unavailable")
			},
			createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
				return storegen.AuditLog{}, nil
			},
		}
		srv := api.NewServer(resumeTestRouter(&dispatched), store, nil)

		resp, err := srv.ResumeWorkflow(context.Background(), gen.ResumeWorkflowRequestObject{
			OrgName: "acme", AppName: "shop", WorkflowId: "wf-1",
		})
		if err != nil {
			t.Fatalf("ResumeWorkflow error: %v", err)
		}
		// The guard fails open so dispatch runs; the stub router
		// succeeds, proving dispatch was reached.
		if _, ok := resp.(gen.ResumeWorkflow204Response); !ok {
			t.Fatalf("expected 204, got %+v", resp)
		}
		if !dispatched {
			t.Error("guard must fail open to dispatch on store errors")
		}
	})

	t.Run("fail-open on corrupt application settings", func(t *testing.T) {
		var audited []string
		var dispatched bool
		store := resumeTestStore(t, `{"latestVersion":`, []storegen.Executor{connectedExec("v1")}, &audited)
		srv := api.NewServer(resumeTestRouter(&dispatched), store, nil)

		resp, err := srv.ResumeWorkflow(context.Background(), gen.ResumeWorkflowRequestObject{
			OrgName: "acme", AppName: "shop", WorkflowId: "wf-1",
		})
		if err != nil {
			t.Fatalf("ResumeWorkflow error: %v", err)
		}
		if _, ok := resp.(gen.ResumeWorkflow204Response); !ok {
			t.Fatalf("expected 204, got %+v", resp)
		}
		if !dispatched {
			t.Error("guard must fail open on corrupt settings")
		}
	})

	t.Run("disconnected executor on latest does not satisfy the guard", func(t *testing.T) {
		var audited []string
		var dispatched bool
		execs := []storegen.Executor{{
			ExecutorID: "exec-2", Status: storegen.ExecutorStatusDisconnected,
			ApplicationVersion: "v2",
		}}
		store := resumeTestStore(t, `{"latestVersion":"v2"}`, execs, &audited)
		// Liveness uses the stored connected status only; display
		// labels such as HEALTHY never satisfy the guard.
		if relaystore.IsLiveStatus(storegen.ExecutorStatus("HEALTHY")) {
			t.Error("display labels must not count as live")
		}
		srv := api.NewServer(resumeTestRouter(&dispatched), store, nil)

		resp, err := srv.ResumeWorkflow(context.Background(), gen.ResumeWorkflowRequestObject{
			OrgName: "acme", AppName: "shop", WorkflowId: "wf-1",
		})
		if err != nil {
			t.Fatalf("ResumeWorkflow error: %v", err)
		}
		if rej, ok := resp.(gen.ResumeWorkflowdefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %+v", resp)
		}
		if dispatched {
			t.Error("dispatch must not run when the guard refuses")
		}
	})

	t.Run("bulk resume refused when latest version has no healthy executor", func(t *testing.T) {
		var audited []string
		var dispatched bool
		store := resumeTestStore(t, `{"latestVersion":"v2"}`, []storegen.Executor{connectedExec("v1")}, &audited)
		srv := api.NewServer(resumeTestRouter(&dispatched), store, nil)

		resp, err := srv.BulkResumeWorkflows(context.Background(), gen.BulkResumeWorkflowsRequestObject{
			OrgName: "acme", AppName: "shop",
			Body: &gen.BulkResumeWorkflowsJSONRequestBody{WorkflowIds: []string{"wf-1"}},
		})
		if err != nil {
			t.Fatalf("BulkResumeWorkflows error: %v", err)
		}
		rej, ok := resp.(gen.BulkResumeWorkflowsdefaultApplicationProblemPlusJSONResponse)
		if !ok || rej.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %+v", resp)
		}
		if dispatched {
			t.Error("dispatch must not run when the guard refuses")
		}
		if len(audited) != 1 || audited[0] != "workflow.bulk_resume" {
			t.Errorf("expected one workflow.bulk_resume failure entry, got %v", audited)
		}
	})
}
