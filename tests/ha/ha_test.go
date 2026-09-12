package ha_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/fakeexecutor"
	"github.com/abn/relay/internal/ha"
	"github.com/abn/relay/internal/hub"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	"github.com/abn/relay/internal/store/gen"
)

type sharedStore struct {
	mu        sync.Mutex
	orgs      map[string]gen.Organisation
	apps      map[string]gen.Application
	appsByID  map[pgtype.UUID]gen.Application
	keys      map[string]gen.ApiKey
	executors map[string]gen.Executor
	instances map[pgtype.UUID]gen.Instance
}

func newSharedStore() *sharedStore {
	return &sharedStore{
		orgs:      make(map[string]gen.Organisation),
		apps:      make(map[string]gen.Application),
		appsByID:  make(map[pgtype.UUID]gen.Application),
		keys:      make(map[string]gen.ApiKey),
		executors: make(map[string]gen.Executor),
		instances: make(map[pgtype.UUID]gen.Instance),
	}
}

func (s *sharedStore) GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	org, ok := s.orgs[name]
	if !ok {
		return gen.Organisation{}, errors.New("org not found")
	}
	return org, nil
}

func (s *sharedStore) GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	app, ok := s.apps[arg.Name]
	if !ok {
		return gen.Application{}, errors.New("app not found")
	}
	return app, nil
}

func (s *sharedStore) CreateApplication(ctx context.Context, arg gen.CreateApplicationParams) (gen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := pgtype.UUID{Bytes: [16]byte{byte(len(s.apps) + 1)}, Valid: true}
	app := gen.Application{
		ID:             id,
		OrganisationID: arg.OrganisationID,
		Name:           arg.Name,
		Settings:       arg.Settings,
	}
	s.apps[arg.Name] = app
	s.appsByID[id] = app
	return app, nil
}

func (s *sharedStore) GetAPIKeyByLookup(ctx context.Context, lookup string) (gen.ApiKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key, ok := s.keys[lookup]
	if !ok {
		return gen.ApiKey{}, errors.New("key not found")
	}
	return key, nil
}

func (s *sharedStore) UpsertExecutor(ctx context.Context, arg gen.UpsertExecutorParams) (gen.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := gen.Executor{
		ApplicationID:      arg.ApplicationID,
		ExecutorID:         arg.ExecutorID,
		ApplicationVersion: arg.ApplicationVersion,
		Hostname:           arg.Hostname,
		Metadata:           arg.Metadata,
		Status:             "connected",
		OwnerInstanceID:    arg.OwnerInstanceID,
		LeaseExpiresAt:     arg.LeaseExpiresAt,
	}
	s.executors[arg.ExecutorID] = e
	return e, nil
}

func (s *sharedStore) DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.executors[arg.ExecutorID]
	if !ok {
		return gen.Executor{}, errors.New("executor not found")
	}
	e.Status = "disconnected"
	s.executors[arg.ExecutorID] = e
	return e, nil
}

func (s *sharedStore) ListConnectedExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []gen.Executor
	for _, e := range s.executors {
		if e.ApplicationID == appID && e.Status == "connected" {
			res = append(res, e)
		}
	}
	return res, nil
}

func (s *sharedStore) UpsertInstance(ctx context.Context, arg gen.UpsertInstanceParams) (gen.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst := gen.Instance{
		ID:               arg.ID,
		AdvertiseAddress: arg.AdvertiseAddress,
		Port:             arg.Port,
	}
	s.instances[arg.ID] = inst
	return inst, nil
}

func (s *sharedStore) GetInstance(ctx context.Context, id pgtype.UUID) (gen.Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return gen.Instance{}, errors.New("instance not found")
	}
	return inst, nil
}

func (s *sharedStore) HeartbeatInstance(ctx context.Context, id pgtype.UUID) error {
	return nil
}

func (s *sharedStore) DeleteInstance(ctx context.Context, id pgtype.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.instances, id)
	return nil
}

func (s *sharedStore) AdoptExpiredExecutors(ctx context.Context, arg gen.AdoptExpiredExecutorsParams) ([]gen.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var adopted []gen.Executor
	for k, e := range s.executors {
		if e.Status == "connected" && (!e.LeaseExpiresAt.Valid || e.LeaseExpiresAt.Time.Before(time.Now())) {
			e.OwnerInstanceID = arg.OwnerInstanceID
			e.LeaseExpiresAt = arg.LeaseExpiresAt
			s.executors[k] = e
			adopted = append(adopted, e)
		}
	}
	return adopted, nil
}

func (s *sharedStore) DeleteStaleInstances(ctx context.Context, cutoff pgtype.Timestamptz) (int64, error) {
	return 0, nil
}

func parseHostPort(addr string) (string, int) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "127.0.0.1", 0
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func TestHA_CrossInstancePeerForwarding(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := newSharedStore()
	secret := []byte("shared-ha-secret")

	// Set up Org, App, and Key
	orgID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	appID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	appName := "order-service"
	orgName := "acme"

	store.orgs[orgName] = gen.Organisation{ID: orgID, Name: orgName}
	store.apps[appName] = gen.Application{ID: appID, OrganisationID: orgID, Name: appName}
	store.appsByID[appID] = store.apps[appName]

	plainKey, rec, err := auth.Mint()
	if err != nil {
		t.Fatalf("Mint failed: %v", err)
	}
	store.keys[rec.Lookup] = gen.ApiKey{
		OrganisationID:   orgID,
		Name:             "test-key",
		Lookup:           rec.Lookup,
		KeyHash:          rec.Hash,
		ApplicationNames: []string{appName},
	}

	node1ID := pgtype.UUID{Bytes: [16]byte{10}, Valid: true}
	node2ID := pgtype.UUID{Bytes: [16]byte{20}, Valid: true}

	cfg := &config.Config{
		ExecutorDeadline: 5 * time.Second,
	}

	// 1. Create Node 1
	hub1 := hub.New(store, cfg, nil)
	defer func() { _ = hub1.Close() }()

	forwardHandler1 := router.NewForwardHandler(hub1, secret, 30*time.Second)
	mux1 := http.NewServeMux()
	mux1.Handle("/websocket/", hub1)
	mux1.Handle("/internal/v1/forward/", forwardHandler1)
	server1 := httptest.NewServer(mux1)
	defer server1.Close()

	s1Host, s1Port := parseHostPort(server1.Listener.Addr().String())
	store.instances[node1ID] = gen.Instance{
		ID:               node1ID,
		AdvertiseAddress: s1Host,
		Port:             int32(s1Port),
	}

	// 2. Create Node 2
	hub2 := hub.New(store, cfg, nil)
	defer func() { _ = hub2.Close() }()

	forwardHandler2 := router.NewForwardHandler(hub2, secret, 30*time.Second)
	mux2 := http.NewServeMux()
	mux2.Handle("/websocket/", hub2)
	mux2.Handle("/internal/v1/forward/", forwardHandler2)
	server2 := httptest.NewServer(mux2)
	defer server2.Close()

	s2Host, s2Port := parseHostPort(server2.Listener.Addr().String())
	store.instances[node2ID] = gen.Instance{
		ID:               node2ID,
		AdvertiseAddress: s2Host,
		Port:             int32(s2Port),
	}

	// Routers
	router1 := router.New(store, hub1)
	forwarder1 := router.NewForwarder(secret, nil)
	router1.SetForwarder(forwarder1, node1ID)

	router2 := router.New(store, hub2)
	forwarder2 := router.NewForwarder(secret, nil)
	router2.SetForwarder(forwarder2, node2ID)

	// 3. Connect Fake Executor to Node 1
	wsURL := "ws" + strings.TrimPrefix(server1.URL, "http")
	exec := fakeexecutor.New(fakeexecutor.Options{
		URL:                wsURL,
		AppName:            appName,
		ConductorKey:       plainKey,
		ExecutorID:         "exec-node-1",
		ApplicationVersion: "v1.0.0",
		Language:           "go",
	})

	// Register get_workflow response on fake executor
	exec.SetHandler(protocol.MessageTypeGetWorkflow, func(msg protocol.Message) (protocol.Message, error) {
		status := "SUCCESS"
		resp := &protocol.GetWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetWorkflow,
				RequestID: msg.GetRequestID(),
			},
			Output: &protocol.ListWorkflowsResponseBody{
				WorkflowUUID: "wf-777",
				Status:       &status,
			},
		}
		return resp, nil
	})

	if err := exec.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer func() { _ = exec.Close() }()

	go func() {
		_ = exec.Run(ctx)
	}()

	// Wait for executor registration
	time.Sleep(50 * time.Millisecond)

	// Manually update store executor owner to Node 1
	store.mu.Lock()
	e := store.executors["exec-node-1"]
	e.OwnerInstanceID = node1ID
	e.LeaseExpiresAt = pgtype.Timestamptz{Time: time.Now().Add(1 * time.Minute), Valid: true}
	store.executors["exec-node-1"] = e
	store.mu.Unlock()

	// 4. Send request to Node 2 (which does NOT own the executor)
	reqMsg := &protocol.GetWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflow,
			RequestID: "req-cross-ha-1",
		},
		WorkflowID: "wf-777",
	}

	res, err := router2.Dispatch(ctx, orgName, appName, reqMsg)
	if err != nil {
		t.Fatalf("Dispatch on Node 2 failed: %v", err)
	}

	getWfRes, ok := res.(*protocol.GetWorkflowResponse)
	if !ok {
		t.Fatalf("unexpected response type: %T", res)
	}
	if getWfRes.Output == nil || getWfRes.Output.WorkflowUUID != "wf-777" || *getWfRes.Output.Status != "SUCCESS" {
		t.Errorf("unexpected response content: %+v", getWfRes)
	}
}

func TestHA_LoopPreventionRefusesSecondHop(t *testing.T) {
	secret := []byte("loop-prevention-secret")

	hubStub := hub.New(newSharedStore(), &config.Config{ExecutorDeadline: time.Second}, nil)
	defer func() { _ = hubStub.Close() }()

	handler := router.NewForwardHandler(hubStub, secret, 30*time.Second)

	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "test-loop"}
	body, _ := protocol.Encode(reqMsg)

	urlPath := fmt.Sprintf("/internal/v1/forward/%s", "05000000-0000-0000-0000-000000000000")
	req, _ := http.NewRequest(http.MethodPost, urlPath, bytes.NewReader(body))

	// Injected hop count 1 (attempted second forward)
	router.SignRequest(req, body, secret, 1, time.Now().Add(5*time.Second))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d", w.Code)
	}
}

func TestHA_UnsignedOrTamperedForwardRefused(t *testing.T) {
	secret := []byte("tamper-secret")
	appID := pgtype.UUID{Bytes: [16]byte{5}, Valid: true}

	hubStub := hub.New(newSharedStore(), &config.Config{ExecutorDeadline: time.Second}, nil)
	defer func() { _ = hubStub.Close() }()

	handler := router.NewForwardHandler(hubStub, secret, 30*time.Second)

	urlPath := fmt.Sprintf("/internal/v1/forward/%s", appID)

	t.Run("UnsignedRequestRejected", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, urlPath, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", w.Code)
		}
	})

	t.Run("TamperedBodyRejected", func(t *testing.T) {
		body := []byte(`{"original":"payload"}`)
		req, _ := http.NewRequest(http.MethodPost, urlPath, bytes.NewReader([]byte(`{"tampered":"payload"}`)))
		router.SignRequest(req, body, secret, 0, time.Now().Add(5*time.Second))

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for tampered payload, got %d", w.Code)
		}
	})
}

func TestHA_InstanceCrashAndLeaseAdoption(t *testing.T) {
	store := newSharedStore()

	crashedNodeID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	survivingNodeID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	// Executor owned by crashed node with expired lease
	store.executors["exec-orphan"] = gen.Executor{
		ExecutorID:      "exec-orphan",
		Status:          "connected",
		OwnerInstanceID: crashedNodeID,
		LeaseExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(-10 * time.Second), Valid: true},
	}

	mgr := ha.NewManager(store, ha.ManagerOptions{
		InstanceID:       survivingNodeID,
		AdvertiseAddress: "127.0.0.1",
		Port:             8091,
		AdoptionInterval: 10 * time.Millisecond,
		LeaseDuration:    1 * time.Minute,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer mgr.Stop()

	time.Sleep(50 * time.Millisecond)

	store.mu.Lock()
	adopted := store.executors["exec-orphan"]
	store.mu.Unlock()

	if adopted.OwnerInstanceID != survivingNodeID {
		t.Errorf("expected adopted owner %v, got %v", survivingNodeID, adopted.OwnerInstanceID)
	}
}


func (m *sharedStore) TouchAPIKeyLastUsed(ctx context.Context, id pgtype.UUID) error {
	return nil
}

func (m *sharedStore) UpsertOrganisation(ctx context.Context, name string) (gen.Organisation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if org, ok := m.orgs[name]; ok {
		return org, nil
	}
	org := gen.Organisation{
		ID:   pgtype.UUID{Bytes: [16]byte{0, 0, 0, byte(len(m.orgs) + 1)}, Valid: true},
		Name: name,
	}
	m.orgs[name] = org
	return org, nil
}
