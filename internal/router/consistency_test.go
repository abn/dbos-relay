package router_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
	"github.com/abn/relay/internal/testdb"
)

type mockConsistencyStore struct {
	org gen.Organisation
	app gen.Application
}

func (m *mockConsistencyStore) GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error) {
	return m.org, nil
}

func (m *mockConsistencyStore) GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
	return m.app, nil
}

type mockConsistencyHub struct {
	onDispatch func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

func (m *mockConsistencyHub) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	if m.onDispatch != nil {
		return m.onDispatch(ctx, appID, msg)
	}
	return nil, errors.New("no live executor connected")
}

type mockDataPlaneClient struct {
	onDispatch func(ctx context.Context, msg protocol.Message) (protocol.Message, error)
}

func (m *mockDataPlaneClient) Dispatch(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
	if m.onDispatch != nil {
		return m.onDispatch(ctx, msg)
	}
	return nil, nil
}

func (m *mockDataPlaneClient) Close() error {
	return nil
}

func TestRouter_DataPlaneFallbackAndConsistency(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4}, Valid: true}
	orgID := pgtype.UUID{Bytes: [16]byte{5, 6, 7, 8}, Valid: true}

	store := &mockConsistencyStore{
		org: gen.Organisation{ID: orgID, Name: "acme"},
		app: gen.Application{ID: appID, Name: "billing", OrganisationID: orgID},
	}

	hub := &mockConsistencyHub{}
	r := router.New(store, hub)

	reqMsg := &protocol.GetWorkflowRequest{
		Envelope:   protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: "req-wf-1"},
		WorkflowID: "wf-100",
	}

	// 1. When no executor is connected and no data-plane configured: returns ErrNoLiveExecutor
	ctx, tracker := router.ContextWithServedFromTracker(context.Background())
	_, err := r.Dispatch(ctx, "acme", "billing", reqMsg)
	if !errors.Is(err, router.ErrNoLiveExecutor) {
		t.Fatalf("expected ErrNoLiveExecutor when no executor and no data plane, got %v", err)
	}
	if tracker.Source != "executor" {
		t.Fatalf("expected default source 'executor', got %s", tracker.Source)
	}

	// 2. Configure data-plane manager with mock client
	mockClient := &mockDataPlaneClient{
		onDispatch: func(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
			return &protocol.GetWorkflowResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: "req-wf-1"},
				Output: &protocol.ListWorkflowsResponseBody{
					WorkflowUUID: "wf-100",
				},
			}, nil
		},
	}
	dpMgr := dataplane.NewManager(func(cfg dataplane.AppConfig) (dataplane.Client, error) {
		return mockClient, nil
	})
	err = dpMgr.RegisterApp(dataplane.AppConfig{
		ApplicationID: appID,
		DatabaseURL:   "postgres://localhost:5432/test",
		Mode:          dataplane.ModeReadWrite,
	})
	if err != nil {
		t.Fatalf("RegisterApp failed: %v", err)
	}
	r.SetDataPlane(dpMgr)

	// 3. Fallback to data-plane when executor is absent
	ctx, tracker = router.ContextWithServedFromTracker(context.Background())
	res, err := r.Dispatch(ctx, "acme", "billing", reqMsg)
	if err != nil {
		t.Fatalf("expected fallback to data-plane to succeed, got %v", err)
	}
	if tracker.Source != "database" {
		t.Fatalf("expected tracker source to be database, got %s", tracker.Source)
	}
	wfRes, ok := res.(*protocol.GetWorkflowResponse)
	if !ok || wfRes.Output == nil || wfRes.Output.WorkflowUUID != "wf-100" {
		t.Fatalf("unexpected data-plane response payload: %+v", res)
	}

	// 4. When executor connects, socket is preferred over data-plane
	hub.onDispatch = func(ctx context.Context, aID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
		return &protocol.GetWorkflowResponse{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: "req-wf-1"},
			Output: &protocol.ListWorkflowsResponseBody{
				WorkflowUUID: "wf-100",
				Status:       ptr("SUCCESS"),
			},
		}, nil
	}

	ctx, tracker = router.ContextWithServedFromTracker(context.Background())
	res, err = r.Dispatch(ctx, "acme", "billing", reqMsg)
	if err != nil {
		t.Fatalf("expected live executor dispatch to succeed, got %v", err)
	}
	if tracker.Source != "executor" {
		t.Fatalf("expected tracker source to be executor when live executor present, got %s", tracker.Source)
	}
	wfRes, ok = res.(*protocol.GetWorkflowResponse)
	if !ok || wfRes.Output == nil || wfRes.Output.Status == nil || *wfRes.Output.Status != "SUCCESS" {
		t.Fatalf("unexpected executor response payload: %+v", res)
	}
}

func TestRouter_DataPlaneFallback_UnsupportedOperationReturns503(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{2, 3, 4, 5}, Valid: true}
	orgID := pgtype.UUID{Bytes: [16]byte{6, 7, 8, 9}, Valid: true}

	store := &mockConsistencyStore{
		org: gen.Organisation{ID: orgID, Name: "acme"},
		app: gen.Application{ID: appID, Name: "billing", OrganisationID: orgID},
	}

	hub := &mockConsistencyHub{
		onDispatch: func(ctx context.Context, aID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, errors.New("no live executor connected")
		},
	}
	r := router.New(store, hub)

	mockClient := &mockDataPlaneClient{
		onDispatch: func(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
			return nil, dataplane.ErrUnsupportedOperation
		},
	}
	dpMgr := dataplane.NewManager(func(cfg dataplane.AppConfig) (dataplane.Client, error) {
		return mockClient, nil
	})
	if err := dpMgr.RegisterApp(dataplane.AppConfig{
		ApplicationID: appID,
		DatabaseURL:   "postgres://localhost:5432/test",
		Mode:          dataplane.ModeReadWrite,
	}); err != nil {
		t.Fatalf("RegisterApp failed: %v", err)
	}
	r.SetDataPlane(dpMgr)

	reqMsg := &protocol.GetWorkflowAggregatesRequest{
		Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflowAggregates, RequestID: "req-agg-fail"},
	}

	ctx := context.Background()
	_, err := r.Dispatch(ctx, "acme", "billing", reqMsg)
	if err == nil {
		t.Fatalf("expected dispatch error, got nil")
	}
	if !errors.Is(err, router.ErrNoLiveExecutor) {
		t.Fatalf("expected ErrNoLiveExecutor for unsupported fallback operation, got %v", err)
	}
}

func ptr(s string) *string {
	return &s
}

func TestRouter_LiveDatabase_DataPlaneFallback(t *testing.T) {
	dbURL, err := testdb.URL("router")
	if err != nil {
		t.Fatalf("failed to derive test db url: %v", err)
	}
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to open live store: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate live store: %v", err)
	}
	defer func() {
		_ = s.Truncate(context.Background())
	}()

	orgName := fmt.Sprintf("router_live_%d", time.Now().UnixNano()%1000000)
	org, err := s.Queries().CreateOrganisation(ctx, orgName)
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	appName := "router-live-app"
	app, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           appName,
		Settings:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	hub := &mockConsistencyHub{}
	r := router.New(s.Queries(), hub)

	reqMsg := &protocol.GetWorkflowRequest{
		Envelope:   protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: "req-live-1"},
		WorkflowID: "wf-live-100",
	}

	// 1. Without executor and without data plane: returns ErrNoLiveExecutor
	dispatchCtx, tracker := router.ContextWithServedFromTracker(ctx)
	_, err = r.Dispatch(dispatchCtx, orgName, appName, reqMsg)
	if !errors.Is(err, router.ErrNoLiveExecutor) {
		t.Fatalf("expected ErrNoLiveExecutor, got %v", err)
	}
	if tracker.Source != "executor" {
		t.Fatalf("expected tracker source 'executor', got %s", tracker.Source)
	}

	// 2. Configure data-plane manager with mock client for Relay-store live test
	mockClient := &mockDataPlaneClient{
		onDispatch: func(ctx context.Context, msg protocol.Message) (protocol.Message, error) {
			return &protocol.GetWorkflowResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: "req-live-1"},
				Output: &protocol.ListWorkflowsResponseBody{
					WorkflowUUID: "wf-live-100",
				},
			}, nil
		},
	}
	dpMgr := dataplane.NewManager(func(cfg dataplane.AppConfig) (dataplane.Client, error) {
		return mockClient, nil
	})
	err = dpMgr.RegisterApp(dataplane.AppConfig{
		ApplicationID: app.ID,
		DatabaseURL:   dbURL,
		Mode:          dataplane.ModeReadWrite,
	})
	if err != nil {
		t.Fatalf("RegisterApp failed: %v", err)
	}
	r.SetDataPlane(dpMgr)

	// 3. Fallback to data-plane executes and records Served-From: database
	dispatchCtx, tracker = router.ContextWithServedFromTracker(ctx)
	res, err := r.Dispatch(dispatchCtx, orgName, appName, reqMsg)
	if err != nil {
		t.Fatalf("expected data-plane fallback to succeed, got %v", err)
	}
	if tracker.Source != "database" {
		t.Fatalf("expected tracker source 'database', got %s", tracker.Source)
	}
	wfRes, ok := res.(*protocol.GetWorkflowResponse)
	if !ok || wfRes.Output == nil || wfRes.Output.WorkflowUUID != "wf-live-100" {
		t.Fatalf("unexpected data-plane response: %+v", res)
	}

	// 4. When live executor connects, executor takes precedence over data-plane
	hub.onDispatch = func(ctx context.Context, aID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
		return &protocol.GetWorkflowResponse{
			Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflow, RequestID: "req-live-1"},
			Output: &protocol.ListWorkflowsResponseBody{
				WorkflowUUID: "wf-live-100",
				Status:       ptr("SUCCESS"),
			},
		}, nil
	}

	dispatchCtx, tracker = router.ContextWithServedFromTracker(ctx)
	res, err = r.Dispatch(dispatchCtx, orgName, appName, reqMsg)
	if err != nil {
		t.Fatalf("expected live executor dispatch to succeed, got %v", err)
	}
	if tracker.Source != "executor" {
		t.Fatalf("expected tracker source 'executor', got %s", tracker.Source)
	}
	wfRes, ok = res.(*protocol.GetWorkflowResponse)
	if !ok || wfRes.Output == nil || wfRes.Output.Status == nil || *wfRes.Output.Status != "SUCCESS" {
		t.Fatalf("unexpected executor response payload: %+v", res)
	}
}
