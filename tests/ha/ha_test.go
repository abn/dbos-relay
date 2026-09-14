// Package ha_test exercises high availability, instance leases, and peer forwarding.
//
// Note: Counterparties in this package are in-process internal/fakeexecutor
// stand-ins. Tests in this package run two hub and router stacks in-process over an
// in-memory store double to verify HTTP forwarding signatures, loop prevention,
// and lease adoption logic. They do not stand up a multi-node deployment behind
// a reverse proxy. Full multi-process Scale tier verification remains open per
// docs/design/compatibility-tiers.md.
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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/fakeexecutor"
	"github.com/abn/relay/internal/ha"
	"github.com/abn/relay/internal/hub"
	"github.com/abn/relay/internal/liveness"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
	"github.com/abn/relay/internal/testdb"
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

func (s *sharedStore) TouchExecutorLastSeen(ctx context.Context, arg gen.TouchExecutorLastSeenParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.executors[arg.ExecutorID]
	if !ok {
		return errors.New("executor not found")
	}
	e.LastSeenAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	e.LeaseExpiresAt = arg.LeaseExpiresAt
	s.executors[arg.ExecutorID] = e
	return nil
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

func (s *sharedStore) ReapExpiredExecutors(ctx context.Context, cutoff pgtype.Timestamptz) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for k, e := range s.executors {
		if e.Status == "disconnected" && e.DisconnectedAt.Valid && e.DisconnectedAt.Time.Before(cutoff.Time) {
			e.Status = "dead"
			s.executors[k] = e
			count++
		}
	}
	return count, nil
}

func parseHostPort(addr string) (string, int) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "127.0.0.1", 0
	}
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func TestHA_CrossInstancePeerForwarding_FakeExecutor(t *testing.T) {
	t.Logf("counterparty: internal/fakeexecutor (in-process stand-in for a DBOS SDK executor)")
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
	hub1.SetInstanceID(node1ID)

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
	hub2.SetInstanceID(node2ID)

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
	t.Log("executor is internal/fakeexecutor protocol stand-in, not a real DBOS SDK")
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

	// Poll until executor registration is complete
	deadline := time.Now().Add(5 * time.Second)
	var registered bool
	for time.Now().Before(deadline) {
		store.mu.Lock()
		_, registered = store.executors["exec-node-1"]
		store.mu.Unlock()
		if registered {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !registered {
		t.Fatal("timed out waiting for executor registration")
	}

	// Verify executor registration has written node1ID ownership and lease
	store.mu.Lock()
	e := store.executors["exec-node-1"]
	store.mu.Unlock()
	if !e.OwnerInstanceID.Valid || e.OwnerInstanceID != node1ID {
		t.Fatalf("expected executor owner to be %v, got %v", node1ID, e.OwnerInstanceID)
	}
	if !e.LeaseExpiresAt.Valid {
		t.Fatal("expected executor lease_expires_at to be valid")
	}

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

	// Poll until lease is adopted
	deadline := time.Now().Add(5 * time.Second)
	var adopted gen.Executor
	for time.Now().Before(deadline) {
		store.mu.Lock()
		adopted = store.executors["exec-orphan"]
		store.mu.Unlock()
		if adopted.OwnerInstanceID == survivingNodeID {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

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

func TestHA_ReapExpiredExecutors_Unit(t *testing.T) {
	store := newSharedStore()

	store.executors["exec-reap-1"] = gen.Executor{
		ExecutorID:     "exec-reap-1",
		Status:         "disconnected",
		DisconnectedAt: pgtype.Timestamptz{Time: time.Now().Add(-2 * time.Hour), Valid: true},
	}

	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Hour), Valid: true}
	count, err := store.ReapExpiredExecutors(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("ReapExpiredExecutors failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 reaped executor, got %d", count)
	}

	if store.executors["exec-reap-1"].Status != "dead" {
		t.Errorf("expected status dead, got %s", store.executors["exec-reap-1"].Status)
	}
}

func TestHA_LiveDatabase_ReapExpiredExecutors(t *testing.T) {
	dbURL, err := testdb.URL("ha")
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
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to open pool: %v", err)
	}
	defer pool.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate store: %v", err)
	}
	defer func() {
		_ = s.Truncate(context.Background())
	}()

	org, err := s.Queries().CreateOrganisation(ctx, fmt.Sprintf("org_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	app, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           "reap-app",
		Settings:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	execID := "exec-live-reap"
	_, err = s.Queries().UpsertExecutor(ctx, gen.UpsertExecutorParams{
		ApplicationID:      app.ID,
		ExecutorID:         execID,
		ApplicationVersion: "v1.0.0",
		Hostname:           "host-1",
		Metadata:           []byte("{}"),
	})
	if err != nil {
		t.Fatalf("failed to upsert executor: %v", err)
	}

	// Update status to disconnected with disconnected_at in the past
	_, err = pool.Exec(ctx, "UPDATE executors SET status = 'disconnected', disconnected_at = now() - interval '2 hours' WHERE executor_id = $1", execID)
	if err != nil {
		t.Fatalf("failed to disconnect executor: %v", err)
	}

	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Hour), Valid: true}
	reapedCount, err := s.Queries().ReapExpiredExecutors(ctx, cutoff)
	if err != nil {
		t.Fatalf("ReapExpiredExecutors query failed: %v", err)
	}
	if reapedCount != 1 {
		t.Errorf("expected 1 reaped executor, got %d", reapedCount)
	}

	execRow, err := s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
		ApplicationID: app.ID,
		ExecutorID:    execID,
	})
	if err != nil {
		t.Fatalf("failed to query executor row: %v", err)
	}
	if execRow.Status != "dead" {
		t.Errorf("expected executor status 'dead', got %q", execRow.Status)
	}
}

func TestHA_LiveDatabase_FailoverAdoptionAndLiveness(t *testing.T) {
	dbURL, err := testdb.URL("ha")
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
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to open pool: %v", err)
	}
	defer pool.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate store: %v", err)
	}
	defer func() {
		_ = s.Truncate(context.Background())
	}()

	org, err := s.Queries().CreateOrganisation(ctx, fmt.Sprintf("org_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	app, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           "failover-app",
		Settings:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	crashedID := pgtype.UUID{Bytes: [16]byte{10}, Valid: true}
	survivingID := pgtype.UUID{Bytes: [16]byte{20}, Valid: true}

	_, err = s.Queries().UpsertInstance(ctx, gen.UpsertInstanceParams{
		ID:               crashedID,
		AdvertiseAddress: "127.0.0.1",
		Port:             8091,
	})
	if err != nil {
		t.Fatalf("failed to insert crashed instance: %v", err)
	}

	orphanID := "exec-orphan-live"
	_, err = s.Queries().UpsertExecutor(ctx, gen.UpsertExecutorParams{
		ApplicationID:      app.ID,
		ExecutorID:         orphanID,
		ApplicationVersion: "v1.0.0",
		Hostname:           "host-crashed",
		Metadata:           []byte("{}"),
	})
	if err != nil {
		t.Fatalf("failed to upsert orphan: %v", err)
	}

	_, err = pool.Exec(ctx, "UPDATE executors SET owner_instance_id = $1, lease_expires_at = now() - interval '10 seconds' WHERE executor_id = $2", crashedID, orphanID)
	if err != nil {
		t.Fatalf("failed to set expired lease: %v", err)
	}

	initRow, err := s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
		ApplicationID: app.ID,
		ExecutorID:    orphanID,
	})
	if err != nil {
		t.Fatalf("failed to get orphan row: %v", err)
	}
	if initRow.OwnerInstanceID != crashedID {
		t.Errorf("expected owner %v, got %v", crashedID, initRow.OwnerInstanceID)
	}

	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	livenessMgr := liveness.NewManager(clock, s.Queries(), nil, nil)
	livenessMgr.SetInstanceID(survivingID)
	defer livenessMgr.Stop()

	haMgr := ha.NewManager(s.Queries(), ha.ManagerOptions{
		InstanceID:        survivingID,
		AdvertiseAddress:  "127.0.0.1",
		Port:              8092,
		HeartbeatInterval: 20 * time.Millisecond,
		AdoptionInterval:  20 * time.Millisecond,
		LeaseDuration:     1 * time.Minute,
		Liveness:          livenessMgr,
	})

	if err := haMgr.Start(ctx); err != nil {
		t.Fatalf("haMgr.Start failed: %v", err)
	}
	defer haMgr.Stop()

	deadline := time.Now().Add(3 * time.Second)
	var adoptedRow gen.Executor
	for time.Now().Before(deadline) {
		adoptedRow, err = s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
			ApplicationID: app.ID,
			ExecutorID:    orphanID,
		})
		if err == nil && adoptedRow.OwnerInstanceID == survivingID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if adoptedRow.OwnerInstanceID != survivingID {
		t.Fatalf("expected adopted owner %v, got %v", survivingID, adoptedRow.OwnerInstanceID)
	}
	if adoptedRow.Status != "connected" {
		t.Errorf("expected status 'connected' upon adoption, got %q", adoptedRow.Status)
	}

	clock.Advance(65 * time.Second)
	time.Sleep(100 * time.Millisecond)

	finalRow, err := s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
		ApplicationID: app.ID,
		ExecutorID:    orphanID,
	})
	if err != nil {
		t.Fatalf("failed to query final row: %v", err)
	}
	if finalRow.Status != "dead" {
		t.Errorf("expected final status 'dead' after grace period, got %q", finalRow.Status)
	}
}

func TestHA_LiveDatabase_LeaseHeartbeatTouch(t *testing.T) {
	t.Logf("counterparty: internal/fakeexecutor (in-process stand-in for a DBOS SDK executor)")
	dbURL, err := testdb.URL("ha")
	if err != nil {
		t.Fatalf("failed to derive test db url: %v", err)
	}
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	s, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate store: %v", err)
	}
	defer func() {
		_ = s.Truncate(context.Background())
	}()

	org, err := s.Queries().CreateOrganisation(ctx, fmt.Sprintf("org_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	appName := "heartbeat-app"
	app, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           appName,
		Settings:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	instID := pgtype.UUID{Bytes: [16]byte{77}, Valid: true}
	_, err = s.Queries().UpsertInstance(ctx, gen.UpsertInstanceParams{
		ID:               instID,
		AdvertiseAddress: "127.0.0.1",
		Port:             8095,
	})
	if err != nil {
		t.Fatalf("failed to upsert instance: %v", err)
	}

	rawKey, keyRec, err := auth.Mint()
	if err != nil {
		t.Fatalf("failed to mint key: %v", err)
	}

	_, err = s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             "test-key",
		Lookup:           keyRec.Lookup,
		KeyHash:          keyRec.Hash,
		ApplicationNames: []string{appName},
		Permissions:      []string{"application.read", "application.write", "websocket.connect"},
	})
	if err != nil {
		t.Fatalf("failed to create api key: %v", err)
	}

	cfg := &config.Config{
		ExecutorDeadline: 5 * time.Second,
	}
	h := hub.New(s.Queries(), cfg, nil)
	defer func() { _ = h.Close() }()

	h.SetInstanceID(instID)
	h.SetLeaseDuration(30 * time.Second)
	h.SetPingPongTimeouts(20*time.Millisecond, 100*time.Millisecond)

	mux := http.NewServeMux()
	mux.HandleFunc("/websocket/", func(w http.ResponseWriter, r *http.Request) {
		pathParts := r.URL.Path[len("/websocket/"):]
		parts := strings.SplitN(pathParts, "/", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		_, conductorKey := parts[0], parts[1]
		rec, err := s.Queries().GetAPIKeyByLookup(r.Context(), auth.Lookup(conductorKey))
		if err != nil || !auth.Verify(conductorKey, rec.KeyHash) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	execID := "exec-heartbeat-test"
	fakeExec := fakeexecutor.New(fakeexecutor.Options{
		URL:                wsURL,
		AppName:            appName,
		ConductorKey:       rawKey,
		ExecutorID:         execID,
		ApplicationVersion: "v1.0.0",
	})

	if err := fakeExec.Connect(ctx); err != nil {
		t.Fatalf("fake executor failed to connect: %v", err)
	}
	go func() { _ = fakeExec.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)

	initialRow, err := s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
		ApplicationID: app.ID,
		ExecutorID:    execID,
	})
	if err != nil {
		t.Fatalf("failed to fetch initial executor row: %v", err)
	}

	if initialRow.OwnerInstanceID != instID {
		t.Errorf("expected owner instance %v, got %v", instID, initialRow.OwnerInstanceID)
	}
	if !initialRow.LeaseExpiresAt.Valid {
		t.Fatal("expected valid LeaseExpiresAt")
	}

	initialExpires := initialRow.LeaseExpiresAt.Time

	time.Sleep(150 * time.Millisecond)

	advancedRow, err := s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
		ApplicationID: app.ID,
		ExecutorID:    execID,
	})
	if err != nil {
		t.Fatalf("failed to fetch advanced executor row: %v", err)
	}

	if !advancedRow.LeaseExpiresAt.Time.After(initialExpires) {
		t.Errorf("expected LeaseExpiresAt to advance with heartbeats: initial=%v, advanced=%v",
			initialExpires, advancedRow.LeaseExpiresAt.Time)
	}
}
