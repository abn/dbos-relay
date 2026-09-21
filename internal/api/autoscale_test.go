package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	storegen "github.com/abn/relay/internal/store/gen"
)

type autoscaleFixture struct {
	orgID      pgtype.UUID
	appID      pgtype.UUID
	policy     *storegen.AutoscalingPolicy
	stored     *storegen.AutoscalingPolicy
	queue      protocol.QueueOutput
	queued     []protocol.ListWorkflowsResponseBody
	settings   string
	execs      []storegen.Executor
	dispatched []protocol.MessageType
}

func (f *autoscaleFixture) store() *mockStoreReader {
	return &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: f.orgID, Name: name}, nil
		},
		getAppByNameFunc: func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
			return storegen.Application{
				ID: f.appID, OrganisationID: f.orgID, Name: "shop",
				Settings: []byte(f.settings),
			}, nil
		},
		listExecutorsByAppFunc: func(ctx context.Context, appID pgtype.UUID) ([]storegen.Executor, error) {
			return f.execs, nil
		},
		getAutoscalingPolicyFunc: func(ctx context.Context, appID pgtype.UUID) (storegen.AutoscalingPolicy, error) {
			if f.policy == nil {
				return storegen.AutoscalingPolicy{}, pgx.ErrNoRows
			}
			return *f.policy, nil
		},
		upsertAutoscalingPolicyFunc: func(ctx context.Context, arg storegen.UpsertAutoscalingPolicyParams) (storegen.AutoscalingPolicy, error) {
			f.stored = &storegen.AutoscalingPolicy{
				ApplicationID: arg.ApplicationID, Queue: arg.Queue,
				MaxOldVersions: arg.MaxOldVersions, MaxExecutorsOld: arg.MaxExecutorsOld,
			}
			f.policy = f.stored
			return *f.stored, nil
		},
		deleteAutoscalingPolicyFunc: func(ctx context.Context, appID pgtype.UUID) (int64, error) {
			f.policy = nil
			return 1, nil
		},
		createAuditLogFunc: func(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
			return storegen.AuditLog{}, nil
		},
	}
}

func (f *autoscaleFixture) router() *mockWorkflowRouter {
	return &mockWorkflowRouter{
		dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
			f.dispatched = append(f.dispatched, msg.GetMessageType())
			switch req := msg.(type) {
			case *protocol.GetQueueRequest:
				q := f.queue
				return &protocol.GetQueueResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeGetQueue, RequestID: req.RequestID},
					Output:   &q,
				}, nil
			case *protocol.ListWorkflowsRequest:
				rows := f.queued
				if req.Body.Offset != nil && *req.Body.Offset > 0 {
					rows = nil
				}
				// ENQUEUED and PENDING listings are disjoint in a real
				// system; the PENDING query returns nothing here.
				if len(req.Body.Status) > 0 {
					rows = nil
				}
				return &protocol.ListWorkflowsResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeListQueuedWorkflows, RequestID: req.RequestID},
					Output:   rows,
				}, nil
			default:
				return nil, errors.New("unexpected message")
			}
		},
	}
}

func newAutoscaleFixture() *autoscaleFixture {
	workerConc := 4
	now := time.Now().UTC()
	appID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	return &autoscaleFixture{
		orgID:    pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		appID:    appID,
		queue:    protocol.QueueOutput{Name: "orders", WorkerConcurrency: &workerConc},
		settings: `{"latestVersion":"v2"}`,
		execs: []storegen.Executor{
			{
				ApplicationID: appID, ExecutorID: "exec-v1", Status: storegen.ExecutorStatusConnected,
				ApplicationVersion: "v1",
				ConnectedAt:        pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
			},
			{
				ApplicationID: appID, ExecutorID: "exec-v2", Status: storegen.ExecutorStatusConnected,
				ApplicationVersion: "v2",
				ConnectedAt:        pgtype.Timestamptz{Time: now, Valid: true},
			},
		},
	}
}

func queuedRow(version string, i int) protocol.ListWorkflowsResponseBody {
	return protocol.ListWorkflowsResponseBody{WorkflowUUID: fmt.Sprintf("wf-%s-%d", version, i), ApplicationVersion: &version}
}

func TestAutoscalingPolicyLifecycle(t *testing.T) {
	t.Run("get without policy returns 404", func(t *testing.T) {
		f := newAutoscaleFixture()
		srv := api.NewServer(f.router(), f.store(), nil)
		resp, err := srv.GetAutoscalingPolicy(context.Background(), gen.GetAutoscalingPolicyRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if rej, ok := resp.(gen.GetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %+v", resp)
		}
	})

	t.Run("put validates queue then stores and echoes", func(t *testing.T) {
		f := newAutoscaleFixture()
		srv := api.NewServer(f.router(), f.store(), nil)
		maxOld := int64(1)
		resp, err := srv.SetAutoscalingPolicy(context.Background(), gen.SetAutoscalingPolicyRequestObject{
			OrgName: "acme", AppName: "shop",
			Body: &gen.SetAutoscalingPolicyJSONRequestBody{
				Queue:   "orders",
				Rollout: &gen.RolloutPolicy{MaxOldApplicationVersions: &maxOld},
			},
		})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		ok, ok2 := resp.(gen.SetAutoscalingPolicy200JSONResponse)
		if !ok2 {
			t.Fatalf("expected 200, got %+v", resp)
		}
		if ok.Policy.Queue != "orders" || ok.Policy.Rollout == nil || *ok.Policy.Rollout.MaxOldApplicationVersions != 1 {
			t.Errorf("unexpected echo: %+v", ok.Policy)
		}
		if f.stored == nil || f.stored.Queue != "orders" {
			t.Errorf("policy not stored: %+v", f.stored)
		}
		sawQueue := false
		for _, mt := range f.dispatched {
			if mt == protocol.MessageTypeGetQueue {
				sawQueue = true
			}
		}
		if !sawQueue {
			t.Error("expected get_queue validation dispatch before store")
		}
	})

	t.Run("put rejects partitioned queue", func(t *testing.T) {
		f := newAutoscaleFixture()
		f.queue.PartitionQueue = true
		srv := api.NewServer(f.router(), f.store(), nil)
		resp, err := srv.SetAutoscalingPolicy(context.Background(), gen.SetAutoscalingPolicyRequestObject{
			OrgName: "acme", AppName: "shop",
			Body: &gen.SetAutoscalingPolicyJSONRequestBody{Queue: "orders"},
		})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if rej, ok := resp.(gen.SetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %+v", resp)
		}
		if f.stored != nil {
			t.Error("invalid policy must not be stored")
		}
	})

	t.Run("put rejects queue without worker concurrency", func(t *testing.T) {
		f := newAutoscaleFixture()
		f.queue.WorkerConcurrency = nil
		srv := api.NewServer(f.router(), f.store(), nil)
		resp, err := srv.SetAutoscalingPolicy(context.Background(), gen.SetAutoscalingPolicyRequestObject{
			OrgName: "acme", AppName: "shop",
			Body: &gen.SetAutoscalingPolicyJSONRequestBody{Queue: "orders"},
		})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if rej, ok := resp.(gen.SetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %+v", resp)
		}
	})

	t.Run("put rejects missing queue name", func(t *testing.T) {
		f := newAutoscaleFixture()
		srv := api.NewServer(f.router(), f.store(), nil)
		resp, err := srv.SetAutoscalingPolicy(context.Background(), gen.SetAutoscalingPolicyRequestObject{
			OrgName: "acme", AppName: "shop",
			Body: &gen.SetAutoscalingPolicyJSONRequestBody{},
		})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if rej, ok := resp.(gen.SetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %+v", resp)
		}
		if len(f.dispatched) != 0 {
			t.Error("no dispatch expected when the request is invalid")
		}
	})

	t.Run("delete turns policy off", func(t *testing.T) {
		f := newAutoscaleFixture()
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders"}
		srv := api.NewServer(f.router(), f.store(), nil)
		resp, err := srv.DeleteAutoscalingPolicy(context.Background(), gen.DeleteAutoscalingPolicyRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if _, ok := resp.(gen.DeleteAutoscalingPolicy204Response); !ok {
			t.Fatalf("expected 204, got %+v", resp)
		}
		if f.policy != nil {
			t.Error("policy not deleted")
		}
		// Deleting again stays 204: the off switch is idempotent.
		resp, err = srv.DeleteAutoscalingPolicy(context.Background(), gen.DeleteAutoscalingPolicyRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if _, ok := resp.(gen.DeleteAutoscalingPolicy204Response); !ok {
			t.Fatalf("expected 204, got %+v", resp)
		}
	})

	t.Run("put without executor returns 503", func(t *testing.T) {
		f := newAutoscaleFixture()
		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
				return nil, fmt.Errorf("dispatch: %w", router.ErrNoLiveExecutor)
			},
		}
		srv := api.NewServer(r, f.store(), nil)
		resp, err := srv.SetAutoscalingPolicy(context.Background(), gen.SetAutoscalingPolicyRequestObject{
			OrgName: "acme", AppName: "shop",
			Body: &gen.SetAutoscalingPolicyJSONRequestBody{Queue: "orders"},
		})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if rej, ok := resp.(gen.SetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %+v", resp)
		}
		if f.stored != nil {
			t.Error("policy must not be stored without executor validation")
		}
	})

	t.Run("put rejects unknown queue", func(t *testing.T) {
		f := newAutoscaleFixture()
		msg := "queue ghost not found"
		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, req protocol.Message) (protocol.Message, error) {
				resp := &protocol.GetQueueResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeGetQueue, RequestID: req.GetRequestID()},
				}
				resp.ErrorMessage = &msg
				return resp, nil
			},
		}
		srv := api.NewServer(r, f.store(), nil)
		resp, err := srv.SetAutoscalingPolicy(context.Background(), gen.SetAutoscalingPolicyRequestObject{
			OrgName: "acme", AppName: "shop",
			Body: &gen.SetAutoscalingPolicyJSONRequestBody{Queue: "ghost"},
		})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if rej, ok := resp.(gen.SetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %+v", resp)
		}
		if f.stored != nil {
			t.Error("policy must not be stored for unknown queue")
		}
	})

	t.Run("put rejects negative rollout caps", func(t *testing.T) {
		for name, rollout := range map[string]*gen.RolloutPolicy{
			"maxOldApplicationVersions":             {MaxOldApplicationVersions: ptrInt64(-1)},
			"maxExecutorsForOldApplicationVersions": {MaxExecutorsForOldApplicationVersions: ptrInt64(-1)},
		} {
			t.Run(name, func(t *testing.T) {
				f := newAutoscaleFixture()
				srv := api.NewServer(f.router(), f.store(), nil)
				resp, err := srv.SetAutoscalingPolicy(context.Background(), gen.SetAutoscalingPolicyRequestObject{
					OrgName: "acme", AppName: "shop",
					Body: &gen.SetAutoscalingPolicyJSONRequestBody{
						Queue:   "orders",
						Rollout: rollout,
					},
				})
				if err != nil {
					t.Fatalf("error: %v", err)
				}
				if rej, ok := resp.(gen.SetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusBadRequest {
					t.Fatalf("expected 400, got %+v", resp)
				}
				if len(f.dispatched) != 0 {
					t.Error("no dispatch expected for invalid caps")
				}
			})
		}
	})
}

func TestAutoscaleRecommendations(t *testing.T) {
	withBacklog := func(f *autoscaleFixture) {
		for i := 0; i < 10; i++ {
			f.queued = append(f.queued, queuedRow("v1", i))
		}
		for i := 0; i < 3; i++ {
			f.queued = append(f.queued, queuedRow("v2", i))
		}
	}

	t.Run("latest minimum one and per-version sizing", func(t *testing.T) {
		f := newAutoscaleFixture()
		withBacklog(f)
		maxOld := int64(5)
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders", MaxOldVersions: &maxOld}
		srv := api.NewServer(f.router(), f.store(), nil)

		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		recs, ok := resp.(gen.GetAutoscale200JSONResponse)
		if !ok || len(recs) != 2 {
			t.Fatalf("expected 2 recommendations, got %+v", resp)
		}
		if !recs[0].IsLatest || recs[0].ApplicationVersion != "v2" {
			t.Errorf("latest first, got %+v", recs[0])
		}
		if recs[0].DesiredExecutors != 1 || recs[0].QueueDepth != 3 {
			t.Errorf("v2: want desired 1 depth 3, got %+v", recs[0])
		}
		if recs[1].DesiredExecutors != 3 || recs[1].QueueDepth != 10 {
			t.Errorf("v1: want desired 3 depth 10, got %+v", recs[1])
		}
		if recs[0].QueueName != "orders" || recs[0].ObservedAt <= 0 {
			t.Errorf("missing queue context: %+v", recs[0])
		}
	})

	t.Run("old versions capped and trimmed", func(t *testing.T) {
		f := newAutoscaleFixture()
		withBacklog(f)
		maxOld := int64(0)
		maxExec := int64(1)
		f.policy = &storegen.AutoscalingPolicy{
			ApplicationID: f.appID, Queue: "orders",
			MaxOldVersions: &maxOld, MaxExecutorsOld: &maxExec,
		}
		srv := api.NewServer(f.router(), f.store(), nil)

		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		recs, ok := resp.(gen.GetAutoscale200JSONResponse)
		if !ok || len(recs) != 1 || !recs[0].IsLatest {
			t.Fatalf("expected only latest, got %+v", resp)
		}
	})

	t.Run("global concurrency caps recommendations", func(t *testing.T) {
		f := newAutoscaleFixture()
		withBacklog(f)
		global := 8
		f.queue.Concurrency = &global
		maxOld := int64(5)
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders", MaxOldVersions: &maxOld}
		srv := api.NewServer(f.router(), f.store(), nil)

		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		recs, ok := resp.(gen.GetAutoscale200JSONResponse)
		if !ok || len(recs) != 2 {
			t.Fatalf("expected 2 recommendations, got %+v", resp)
		}
		if recs[1].DesiredExecutors != 2 {
			t.Errorf("v1 capped at ceil(8/4)=2, got %+v", recs[1])
		}
	})

	t.Run("drained old versions omitted", func(t *testing.T) {
		f := newAutoscaleFixture()
		f.queued = []protocol.ListWorkflowsResponseBody{queuedRow("v2", 0)}
		maxOld := int64(5)
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders", MaxOldVersions: &maxOld}
		srv := api.NewServer(f.router(), f.store(), nil)

		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		recs, ok := resp.(gen.GetAutoscale200JSONResponse)
		if !ok || len(recs) != 1 || !recs[0].IsLatest {
			t.Fatalf("expected only latest, got %+v", resp)
		}
	})

	t.Run("version endpoint resolves latest and rejects unknown", func(t *testing.T) {
		f := newAutoscaleFixture()
		withBacklog(f)
		maxOld := int64(5)
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders", MaxOldVersions: &maxOld}
		srv := api.NewServer(f.router(), f.store(), nil)

		resp, err := srv.GetAutoscaleVersion(context.Background(), gen.GetAutoscaleVersionRequestObject{OrgName: "acme", AppName: "shop", Version: "latest"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		rec, ok := resp.(gen.GetAutoscaleVersion200JSONResponse)
		if !ok || !rec.IsLatest || rec.ApplicationVersion != "v2" {
			t.Fatalf("expected v2 latest, got %+v", resp)
		}

		resp, err = srv.GetAutoscaleVersion(context.Background(), gen.GetAutoscaleVersionRequestObject{OrgName: "acme", AppName: "shop", Version: "v9"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if rej, ok := resp.(gen.GetAutoscaleVersiondefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %+v", resp)
		}
	})

	t.Run("version endpoint applies old-version cap", func(t *testing.T) {
		f := newAutoscaleFixture()
		withBacklog(f)
		maxOld := int64(5)
		maxExec := int64(1)
		f.policy = &storegen.AutoscalingPolicy{
			ApplicationID: f.appID, Queue: "orders",
			MaxOldVersions: &maxOld, MaxExecutorsOld: &maxExec,
		}
		srv := api.NewServer(f.router(), f.store(), nil)

		// maxOldApplicationVersions does not apply to this endpoint,
		// but the per-old-version cap does: v1 depth 10 at worker
		// concurrency 4 wants 3, capped to 1.
		resp, err := srv.GetAutoscaleVersion(context.Background(), gen.GetAutoscaleVersionRequestObject{OrgName: "acme", AppName: "shop", Version: "v1"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		rec, ok := resp.(gen.GetAutoscaleVersion200JSONResponse)
		if !ok || rec.DesiredExecutors != 1 || rec.QueueDepth != 10 {
			t.Fatalf("expected capped v1 rec, got %+v", resp)
		}
	})

	t.Run("version endpoint reports drained old versions at zero", func(t *testing.T) {
		f := newAutoscaleFixture()
		f.queued = []protocol.ListWorkflowsResponseBody{queuedRow("v2", 0)}
		maxOld := int64(5)
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders", MaxOldVersions: &maxOld}
		srv := api.NewServer(f.router(), f.store(), nil)

		// v1 is known from executor records but has no backlog: the
		// all-versions endpoint omits it, the version endpoint reports
		// zero instead of 404.
		resp, err := srv.GetAutoscaleVersion(context.Background(), gen.GetAutoscaleVersionRequestObject{OrgName: "acme", AppName: "shop", Version: "v1"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		rec, ok := resp.(gen.GetAutoscaleVersion200JSONResponse)
		if !ok || rec.DesiredExecutors != 0 || rec.QueueDepth != 0 || rec.IsLatest {
			t.Fatalf("expected zeroed v1 rec, got %+v", resp)
		}
	})

	t.Run("autoscale without policy returns 404", func(t *testing.T) {
		f := newAutoscaleFixture()
		srv := api.NewServer(f.router(), f.store(), nil)
		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if rej, ok := resp.(gen.GetAutoscaledefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %+v", resp)
		}
		if len(f.dispatched) != 0 {
			t.Error("no dispatch expected without a stored policy")
		}
	})

	t.Run("get autoscale without executor returns 503", func(t *testing.T) {
		f := newAutoscaleFixture()
		maxOld := int64(5)
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders", MaxOldVersions: &maxOld}
		r := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
				return nil, fmt.Errorf("dispatch: %w", router.ErrNoLiveExecutor)
			},
		}
		srv := api.NewServer(r, f.store(), nil)
		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if rej, ok := resp.(gen.GetAutoscaledefaultApplicationProblemPlusJSONResponse); !ok || rej.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %+v", resp)
		}
	})

	t.Run("latest falls back to most recently connected executor", func(t *testing.T) {
		f := newAutoscaleFixture()
		f.settings = `{}`
		f.queued = []protocol.ListWorkflowsResponseBody{queuedRow("v1", 0), queuedRow("v2", 0)}
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders"}
		srv := api.NewServer(f.router(), f.store(), nil)

		// No recorded latest: v2 connected most recently, so v2 leads
		// with minimum one and v1 is trimmed by default maxOld 0.
		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		recs, ok := resp.(gen.GetAutoscale200JSONResponse)
		if !ok || len(recs) != 1 || !recs[0].IsLatest || recs[0].ApplicationVersion != "v2" {
			t.Fatalf("expected v2 latest, got %+v", resp)
		}
	})

	t.Run("latest falls back to lexicographic maximum without executors", func(t *testing.T) {
		f := newAutoscaleFixture()
		f.settings = `{}`
		f.execs = nil
		// v9 sorts after v10 as strings (numeric order would disagree),
		// pinning the documented lexicographic fallback.
		f.queued = []protocol.ListWorkflowsResponseBody{queuedRow("v10", 0), queuedRow("v9", 0)}
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders"}
		srv := api.NewServer(f.router(), f.store(), nil)

		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		recs, ok := resp.(gen.GetAutoscale200JSONResponse)
		if !ok || len(recs) != 1 || !recs[0].IsLatest {
			t.Fatalf("expected single latest, got %+v", resp)
		}
		if recs[0].ApplicationVersion != "v9" {
			t.Fatalf("expected lexicographic maximum v9, got %+v", recs[0])
		}
	})

	t.Run("empty world returns empty list", func(t *testing.T) {
		f := newAutoscaleFixture()
		f.settings = `{}`
		f.execs = nil
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders"}
		srv := api.NewServer(f.router(), f.store(), nil)

		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		recs, ok := resp.(gen.GetAutoscale200JSONResponse)
		if !ok || len(recs) != 0 {
			t.Fatalf("expected empty list, got %+v", resp)
		}
	})

	t.Run("overlapping listings do not double-count", func(t *testing.T) {
		f := newAutoscaleFixture()
		// An executor that reports the same workflows in both the
		// ENQUEUED and PENDING listings must not inflate the depth:
		// rows are deduplicated by workflow UUID.
		rows := []protocol.ListWorkflowsResponseBody{queuedRow("v2", 0), queuedRow("v2", 1)}
		overlap := &mockWorkflowRouter{
			dispatchFunc: func(_ context.Context, _, _ string, msg protocol.Message) (protocol.Message, error) {
				switch req := msg.(type) {
				case *protocol.GetQueueRequest:
					q := f.queue
					return &protocol.GetQueueResponse{
						Envelope: protocol.Envelope{Type: protocol.MessageTypeGetQueue, RequestID: req.RequestID},
						Output:   &q,
					}, nil
				case *protocol.ListWorkflowsRequest:
					out := rows
					if req.Body.Offset != nil && *req.Body.Offset > 0 {
						out = nil
					}
					return &protocol.ListWorkflowsResponse{
						Envelope: protocol.Envelope{Type: protocol.MessageTypeListQueuedWorkflows, RequestID: req.GetRequestID()},
						Output:   out,
					}, nil
				default:
					return nil, errors.New("unexpected message")
				}
			},
		}
		maxOld := int64(5)
		f.policy = &storegen.AutoscalingPolicy{ApplicationID: f.appID, Queue: "orders", MaxOldVersions: &maxOld}
		srv := api.NewServer(overlap, f.store(), nil)

		resp, err := srv.GetAutoscale(context.Background(), gen.GetAutoscaleRequestObject{OrgName: "acme", AppName: "shop"})
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		recs, ok := resp.(gen.GetAutoscale200JSONResponse)
		if !ok || len(recs) != 1 {
			t.Fatalf("expected 1 recommendation, got %+v", resp)
		}
		if recs[0].QueueDepth != 2 {
			t.Errorf("depth = %d, want 2 (not double-counted)", recs[0].QueueDepth)
		}
	})
}

func ptrInt64(v int64) *int64 { return &v }
