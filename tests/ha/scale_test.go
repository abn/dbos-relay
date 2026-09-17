// Package ha_test exercises high availability, multi-instance scale clustering,
// lease adoption, and peer request forwarding.
//
// Note: Counterparties in this test file are in-process internal/fakeexecutor
// stand-ins. These tests verify multi-instance clustering, lease adoption,
// reverse proxy routing, and cross-instance peer forwarding.
package ha_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/fakeexecutor"
	"github.com/abn/relay/internal/ha"
	"github.com/abn/relay/internal/hub"
	"github.com/abn/relay/internal/metrics"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
	"github.com/abn/relay/internal/testdb"
)

// ListAllApplications satisfies metrics.Store for sharedStore.
func (s *sharedStore) ListAllApplications(ctx context.Context) ([]gen.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []gen.Application
	for _, a := range s.apps {
		res = append(res, a)
	}
	return res, nil
}

// ListExecutorsByApplication satisfies metrics.Store for sharedStore.
func (s *sharedStore) ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res []gen.Executor
	for _, e := range s.executors {
		if e.ApplicationID == appID {
			res = append(res, e)
		}
	}
	return res, nil
}

func TestScale_MultiInstanceClustering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	storeDouble := newSharedStore()

	node1ID := pgtype.UUID{Bytes: [16]byte{1, 1, 1, 1}, Valid: true}
	node2ID := pgtype.UUID{Bytes: [16]byte{2, 2, 2, 2}, Valid: true}

	mgr1 := ha.NewManager(storeDouble, ha.ManagerOptions{
		InstanceID:        node1ID,
		AdvertiseAddress:  "relay-node-1",
		Port:              8091,
		HeartbeatInterval: 25 * time.Millisecond,
		AdoptionInterval:  50 * time.Millisecond,
		LeaseDuration:     1 * time.Minute,
	})

	mgr2 := ha.NewManager(storeDouble, ha.ManagerOptions{
		InstanceID:        node2ID,
		AdvertiseAddress:  "relay-node-2",
		Port:              8092,
		HeartbeatInterval: 25 * time.Millisecond,
		AdoptionInterval:  50 * time.Millisecond,
		LeaseDuration:     1 * time.Minute,
	})

	if err := mgr1.Start(ctx); err != nil {
		t.Fatalf("mgr1.Start failed: %v", err)
	}
	defer mgr1.Stop()

	if err := mgr2.Start(ctx); err != nil {
		t.Fatalf("mgr2.Start failed: %v", err)
	}
	defer mgr2.Stop()

	// Verify both nodes are registered in the cluster store
	inst1, err := storeDouble.GetInstance(ctx, node1ID)
	if err != nil {
		t.Fatalf("storeDouble.GetInstance(node1) failed: %v", err)
	}
	if inst1.AdvertiseAddress != "relay-node-1" || inst1.Port != 8091 {
		t.Errorf("unexpected node1 registration: %+v", inst1)
	}

	inst2, err := storeDouble.GetInstance(ctx, node2ID)
	if err != nil {
		t.Fatalf("storeDouble.GetInstance(node2) failed: %v", err)
	}
	if inst2.AdvertiseAddress != "relay-node-2" || inst2.Port != 8092 {
		t.Errorf("unexpected node2 registration: %+v", inst2)
	}

	// Verify stopping node1 cleans up its registration
	mgr1.Stop()
	storeDouble.mu.Lock()
	_, exists := storeDouble.instances[node1ID]
	storeDouble.mu.Unlock()
	if exists {
		t.Errorf("expected node1 to be removed from instances upon Stop")
	}

	// Verify node2 remains registered
	storeDouble.mu.Lock()
	_, exists = storeDouble.instances[node2ID]
	storeDouble.mu.Unlock()
	if !exists {
		t.Errorf("expected node2 to remain registered")
	}
}

func TestScale_DualInstanceRequestForwarding_FakeExecutor(t *testing.T) {
	t.Log("counterparty: internal/fakeexecutor (in-process stand-in for an SDK executor)")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	storeDouble := newSharedStore()
	sharedSecret := []byte("scale-cluster-shared-secret")

	orgID := pgtype.UUID{Bytes: [16]byte{0xaa}, Valid: true}
	appID := pgtype.UUID{Bytes: [16]byte{0xbb}, Valid: true}
	appName := "checkout-service"
	orgName := "enterprise"

	storeDouble.orgs[orgName] = gen.Organisation{ID: orgID, Name: orgName}
	storeDouble.apps[appName] = gen.Application{ID: appID, OrganisationID: orgID, Name: appName}
	storeDouble.appsByID[appID] = storeDouble.apps[appName]

	plainKey, keyRec, err := auth.Mint()
	if err != nil {
		t.Fatalf("auth.Mint failed: %v", err)
	}
	storeDouble.keys[keyRec.Lookup] = gen.ApiKey{
		OrganisationID:   orgID,
		Name:             "scale-key",
		Lookup:           keyRec.Lookup,
		KeyHash:          keyRec.Hash,
		ApplicationNames: []string{appName},
	}

	node1ID := pgtype.UUID{Bytes: [16]byte{0x10}, Valid: true}
	node2ID := pgtype.UUID{Bytes: [16]byte{0x20}, Valid: true}

	cfg := &config.Config{
		ExecutorDeadline: 5 * time.Second,
	}

	// Instance 1: owns executor
	hub1 := hub.New(storeDouble, cfg, nil)
	defer func() { _ = hub1.Close() }()
	hub1.SetInstanceID(node1ID)

	forwardHandler1 := router.NewForwardHandler(hub1, sharedSecret, 30*time.Second)
	mux1 := http.NewServeMux()
	mux1.Handle("/websocket/", hub1)
	mux1.Handle("/internal/v1/forward/", forwardHandler1)
	mux1.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	server1 := httptest.NewServer(mux1)
	defer server1.Close()

	s1Host, s1Port := parseHostPort(server1.Listener.Addr().String())
	storeDouble.instances[node1ID] = gen.Instance{
		ID:               node1ID,
		AdvertiseAddress: s1Host,
		Port:             int32(s1Port),
	}

	// Instance 2: routes requests to peer
	hub2 := hub.New(storeDouble, cfg, nil)
	defer func() { _ = hub2.Close() }()
	hub2.SetInstanceID(node2ID)

	forwardHandler2 := router.NewForwardHandler(hub2, sharedSecret, 30*time.Second)
	mux2 := http.NewServeMux()
	mux2.Handle("/websocket/", hub2)
	mux2.Handle("/internal/v1/forward/", forwardHandler2)
	mux2.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	server2 := httptest.NewServer(mux2)
	defer server2.Close()

	s2Host, s2Port := parseHostPort(server2.Listener.Addr().String())
	storeDouble.instances[node2ID] = gen.Instance{
		ID:               node2ID,
		AdvertiseAddress: s2Host,
		Port:             int32(s2Port),
	}

	router2 := router.New(storeDouble, hub2)
	forwarder2 := router.NewForwarder(sharedSecret, nil)
	router2.SetForwarder(forwarder2, node2ID)

	// Connect fake executor to Node 1
	wsURL := "ws" + strings.TrimPrefix(server1.URL, "http")
	execID := "exec-scale-worker-1"
	fakeExec := fakeexecutor.New(fakeexecutor.Options{
		URL:                wsURL,
		AppName:            appName,
		ConductorKey:       plainKey,
		ExecutorID:         execID,
		ApplicationVersion: "v1.0.0",
		Language:           "go",
	})

	fakeExec.SetHandler(protocol.MessageTypeGetWorkflow, func(msg protocol.Message) (protocol.Message, error) {
		status := "SUCCESS"
		return &protocol.GetWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetWorkflow,
				RequestID: msg.GetRequestID(),
			},
			Output: &protocol.ListWorkflowsResponseBody{
				WorkflowUUID: "scale-wf-101",
				Status:       &status,
			},
		}, nil
	})

	if err := fakeExec.Connect(ctx); err != nil {
		t.Fatalf("fakeExec.Connect failed: %v", err)
	}
	defer func() { _ = fakeExec.Close() }()

	go func() {
		_ = fakeExec.Run(ctx)
	}()

	// Wait for executor registration in store
	deadline := time.Now().Add(5 * time.Second)
	var registered bool
	for time.Now().Before(deadline) {
		storeDouble.mu.Lock()
		_, registered = storeDouble.executors[execID]
		storeDouble.mu.Unlock()
		if registered {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !registered {
		t.Fatal("timed out waiting for executor registration")
	}

	// Verify executor ownership is assigned to Node 1
	storeDouble.mu.Lock()
	execRec := storeDouble.executors[execID]
	storeDouble.mu.Unlock()
	if execRec.OwnerInstanceID != node1ID {
		t.Fatalf("expected executor owner %v, got %v", node1ID, execRec.OwnerInstanceID)
	}

	// Dispatch request to Node 2 (forwarding to Node 1)
	reqMsg := &protocol.GetWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflow,
			RequestID: "scale-req-forward-1",
		},
		WorkflowID: "scale-wf-101",
	}

	res, err := router2.Dispatch(ctx, orgName, appName, reqMsg)
	if err != nil {
		t.Fatalf("router2.Dispatch failed: %v", err)
	}

	wfResp, ok := res.(*protocol.GetWorkflowResponse)
	if !ok {
		t.Fatalf("unexpected response type: %T", res)
	}
	if wfResp.Output == nil || wfResp.Output.WorkflowUUID != "scale-wf-101" {
		t.Fatalf("unexpected workflow response output: %+v", wfResp.Output)
	}

	// Verify loop prevention refuses hop >= 1
	t.Run("LoopPrevention", func(t *testing.T) {
		loopMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "loop-check"}
		body, _ := protocol.Encode(loopMsg)
		urlPath := fmt.Sprintf("%s/internal/v1/forward/%s", server1.URL, appID)
		req, _ := http.NewRequest(http.MethodPost, urlPath, bytes.NewReader(body))
		router.SignRequest(req, body, sharedSecret, 1, time.Now().Add(5*time.Second))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("loop request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409 Conflict for hop>=1, got %d", resp.StatusCode)
		}
	})

	// Verify tampered signature is rejected
	t.Run("TamperResistance", func(t *testing.T) {
		body := []byte(`{"valid":"payload"}`)
		urlPath := fmt.Sprintf("%s/internal/v1/forward/%s", server1.URL, appID)
		req, _ := http.NewRequest(http.MethodPost, urlPath, bytes.NewReader(body))
		router.SignRequest(req, body, []byte("wrong-secret"), 0, time.Now().Add(5*time.Second))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("tamper request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for bad signature, got %d", resp.StatusCode)
		}
	})
}

func TestScale_DualInstanceLeaseAdoptionAndFailover(t *testing.T) {
	t.Log("counterparty: internal/fakeexecutor (in-process stand-in for an SDK executor)")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	storeDouble := newSharedStore()

	node1ID := pgtype.UUID{Bytes: [16]byte{1, 1}, Valid: true}
	node2ID := pgtype.UUID{Bytes: [16]byte{2, 2}, Valid: true}
	orgID := pgtype.UUID{Bytes: [16]byte{3, 3}, Valid: true}
	appID := pgtype.UUID{Bytes: [16]byte{4, 4}, Valid: true}
	appName := "payment-worker"
	orgName := "enterprise"

	storeDouble.orgs[orgName] = gen.Organisation{ID: orgID, Name: orgName}
	storeDouble.apps[appName] = gen.Application{ID: appID, OrganisationID: orgID, Name: appName}
	storeDouble.appsByID[appID] = storeDouble.apps[appName]

	orphanExecID := "exec-scale-orphan"
	storeDouble.executors[orphanExecID] = gen.Executor{
		ApplicationID:   appID,
		ExecutorID:      orphanExecID,
		Status:          "connected",
		OwnerInstanceID: node1ID,
		LeaseExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(-5 * time.Second), Valid: true},
	}

	// Start Node 2 HA manager to adopt expired leases
	mgr2 := ha.NewManager(storeDouble, ha.ManagerOptions{
		InstanceID:       node2ID,
		AdvertiseAddress: "relay-node-2",
		Port:             8092,
		AdoptionInterval: 10 * time.Millisecond,
		LeaseDuration:    30 * time.Second,
	})

	if err := mgr2.Start(ctx); err != nil {
		t.Fatalf("mgr2.Start failed: %v", err)
	}
	defer mgr2.Stop()

	// Wait for Node 2 to adopt the orphaned executor lease
	deadline := time.Now().Add(5 * time.Second)
	var adopted gen.Executor
	for time.Now().Before(deadline) {
		storeDouble.mu.Lock()
		adopted = storeDouble.executors[orphanExecID]
		storeDouble.mu.Unlock()
		if adopted.OwnerInstanceID == node2ID {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if adopted.OwnerInstanceID != node2ID {
		t.Fatalf("expected executor adopted by %v, got %v", node2ID, adopted.OwnerInstanceID)
	}
	if !adopted.LeaseExpiresAt.Time.After(time.Now()) {
		t.Errorf("expected renewed lease expiration, got %v", adopted.LeaseExpiresAt.Time)
	}
}

func TestScale_ReverseProxy_RoutingAndMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	storeDouble := newSharedStore()

	orgID := pgtype.UUID{Bytes: [16]byte{0x10}, Valid: true}
	appID := pgtype.UUID{Bytes: [16]byte{0x20}, Valid: true}
	storeDouble.orgs["default"] = gen.Organisation{ID: orgID, Name: "default"}
	storeDouble.apps["scale-app"] = gen.Application{ID: appID, OrganisationID: orgID, Name: "scale-app"}
	storeDouble.executors["exec-scale-metrics"] = gen.Executor{
		ApplicationID:      appID,
		ExecutorID:         "exec-scale-metrics",
		ApplicationVersion: "v1.2.3",
		Status:             "connected",
	}

	// Backend Node 1
	metricsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.WithIdentity(r.Context(), &auth.UserIdentity{
			Subject: "metrics-scraper",
			OrgName: "default",
			IsAdmin: true,
		})
		metrics.NewHandler(storeDouble).ServeHTTP(w, r.WithContext(ctx))
	})
	mux1 := http.NewServeMux()
	mux1.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux1.Handle("/v1/metrics", metricsHandler)
	mux1.HandleFunc("/websocket/", func(w http.ResponseWriter, r *http.Request) {
		if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
			w.Header().Set("X-WebSocket-Upgraded", "true")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("upgraded"))
			return
		}
		http.Error(w, "expected websocket upgrade", http.StatusBadRequest)
	})
	mux1.HandleFunc("/internal/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("internal-ok"))
	})
	backendServer1 := httptest.NewServer(mux1)
	defer backendServer1.Close()

	// Reverse proxy fronting the cluster (mirroring nginx.conf)
	u1, _ := url.Parse(backendServer1.URL)
	reverseProxy := httputil.NewSingleHostReverseProxy(u1)
	var proxyReqCount uint64

	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&proxyReqCount, 1)

		// Mirror nginx.conf: block external access to /internal/
		if strings.HasPrefix(r.URL.Path, "/internal/") {
			http.Error(w, "Forbidden: internal forwarding endpoint\n", http.StatusForbidden)
			return
		}

		reverseProxy.ServeHTTP(w, r)
	})

	proxyServer := httptest.NewServer(proxyHandler)
	defer proxyServer.Close()

	// 1. Health check via proxy
	t.Run("HealthCheckViaProxy", func(t *testing.T) {
		resp, err := http.Get(proxyServer.URL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
			t.Errorf("unexpected health check response: status=%d body=%s", resp.StatusCode, string(body))
		}
	})

	// 2. Metrics scrape via proxy (Scale compatibility tier gate verification)
	t.Run("MetricsScrapeViaProxy", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, proxyServer.URL+"/v1/metrics", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /v1/metrics failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK from metrics scrape, got %d: %s", resp.StatusCode, string(body))
		}
		bodyStr := string(body)
		if !strings.Contains(bodyStr, "executor_count") {
			t.Errorf("expected metrics to include executor_count, got:\n%s", bodyStr)
		}
	})

	// 3. WebSocket upgrade via proxy
	t.Run("WebSocketUpgradeRouting", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, proxyServer.URL+"/websocket/scale-app/token", nil)
		req.Header.Set("Upgrade", "websocket")
		req.Header.Set("Connection", "Upgrade")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("websocket probe failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.Header.Get("X-WebSocket-Upgraded") != "true" {
			t.Errorf("expected upgrade header preserved through proxy")
		}
	})

	// 4. Internal forward blocked at proxy boundary
	t.Run("InternalEndpointsBlockedAtProxy", func(t *testing.T) {
		resp, err := http.Post(proxyServer.URL+"/internal/v1/forward/some-app", "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatalf("internal forward probe failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for external /internal/ access, got %d", resp.StatusCode)
		}
	})
}

func TestScale_LiveDatabase_DualInstanceCluster(t *testing.T) {
	t.Log("counterparty: internal/fakeexecutor (in-process stand-in for an SDK executor)")
	dbURL, err := testdb.URL("ha")
	if err != nil {
		t.Fatalf("testdb.URL failed: %v", err)
	}
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	s, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	defer s.Close()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool.New failed: %v", err)
	}
	defer pool.Close()

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("s.Migrate failed: %v", err)
	}
	defer func() {
		_ = s.Truncate(context.Background())
	}()

	org, err := s.Queries().CreateOrganisation(ctx, fmt.Sprintf("scale_org_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("CreateOrganisation failed: %v", err)
	}

	appName := fmt.Sprintf("scale_app_%d", time.Now().UnixNano())
	app, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           appName,
		Settings:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateApplication failed: %v", err)
	}

	node1ID := pgtype.UUID{Bytes: [16]byte{0x31}, Valid: true}
	node2ID := pgtype.UUID{Bytes: [16]byte{0x32}, Valid: true}

	rawKey, keyRec, err := auth.Mint()
	if err != nil {
		t.Fatalf("auth.Mint failed: %v", err)
	}
	_, err = s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             "scale-key",
		Lookup:           keyRec.Lookup,
		KeyHash:          keyRec.Hash,
		ApplicationNames: []string{appName},
		Permissions:      []string{"application.read", "application.write", "websocket.connect"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	sharedSecret := []byte("scale-live-db-secret")

	// Node 1
	cfg1 := &config.Config{ExecutorDeadline: 5 * time.Second}
	hub1 := hub.New(s.Queries(), cfg1, nil)
	defer func() { _ = hub1.Close() }()
	hub1.SetInstanceID(node1ID)

	forwardHandler1 := router.NewForwardHandler(hub1, sharedSecret, 30*time.Second)
	mux1 := http.NewServeMux()
	mux1.HandleFunc("/websocket/", func(w http.ResponseWriter, r *http.Request) {
		pathParts := r.URL.Path[len("/websocket/"):]
		parts := strings.SplitN(pathParts, "/", 2)
		if len(parts) == 2 && auth.Verify(parts[1], keyRec.Hash) {
			hub1.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	mux1.Handle("/internal/v1/forward/", forwardHandler1)
	server1 := httptest.NewServer(mux1)
	defer server1.Close()

	s1Host, s1Port := parseHostPort(server1.Listener.Addr().String())
	_, err = s.Queries().UpsertInstance(ctx, gen.UpsertInstanceParams{
		ID:               node1ID,
		AdvertiseAddress: s1Host,
		Port:             int32(s1Port),
	})
	if err != nil {
		t.Fatalf("upsert node1 instance failed: %v", err)
	}

	// Node 2
	cfg2 := &config.Config{ExecutorDeadline: 5 * time.Second}
	hub2 := hub.New(s.Queries(), cfg2, nil)
	defer func() { _ = hub2.Close() }()
	hub2.SetInstanceID(node2ID)

	forwardHandler2 := router.NewForwardHandler(hub2, sharedSecret, 30*time.Second)
	mux2 := http.NewServeMux()
	mux2.Handle("/internal/v1/forward/", forwardHandler2)
	server2 := httptest.NewServer(mux2)
	defer server2.Close()

	s2Host, s2Port := parseHostPort(server2.Listener.Addr().String())
	_, err = s.Queries().UpsertInstance(ctx, gen.UpsertInstanceParams{
		ID:               node2ID,
		AdvertiseAddress: s2Host,
		Port:             int32(s2Port),
	})
	if err != nil {
		t.Fatalf("upsert node2 instance failed: %v", err)
	}

	router2 := router.New(s.Queries(), hub2)
	forwarder2 := router.NewForwarder(sharedSecret, nil)
	router2.SetForwarder(forwarder2, node2ID)

	// Connect fake executor to Node 1
	wsURL := "ws" + strings.TrimPrefix(server1.URL, "http")
	execID := "exec-live-scale-worker"
	fakeExec := fakeexecutor.New(fakeexecutor.Options{
		URL:                wsURL,
		AppName:            appName,
		ConductorKey:       rawKey,
		ExecutorID:         execID,
		ApplicationVersion: "v1.0.0",
	})
	fakeExec.SetHandler(protocol.MessageTypeGetWorkflow, func(msg protocol.Message) (protocol.Message, error) {
		status := "PENDING"
		return &protocol.GetWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetWorkflow,
				RequestID: msg.GetRequestID(),
			},
			Output: &protocol.ListWorkflowsResponseBody{
				WorkflowUUID: "live-wf-42",
				Status:       &status,
			},
		}, nil
	})

	if err := fakeExec.Connect(ctx); err != nil {
		t.Fatalf("fakeExec.Connect failed: %v", err)
	}
	defer func() { _ = fakeExec.Close() }()
	go func() { _ = fakeExec.Run(ctx) }()

	// Poll until executor appears in PostgreSQL
	deadline := time.Now().Add(5 * time.Second)
	var liveExecRow gen.Executor
	for time.Now().Before(deadline) {
		liveExecRow, err = s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
			ApplicationID: app.ID,
			ExecutorID:    execID,
		})
		if err == nil && liveExecRow.OwnerInstanceID == node1ID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if liveExecRow.OwnerInstanceID != node1ID {
		t.Fatalf("expected executor owned by %v in database, got %v", node1ID, liveExecRow.OwnerInstanceID)
	}

	// Dispatch request through Node 2
	res, err := router2.Dispatch(ctx, org.Name, appName, &protocol.GetWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetWorkflow,
			RequestID: "live-req-forward-1",
		},
		WorkflowID: "live-wf-42",
	})
	if err != nil {
		t.Fatalf("router2.Dispatch with peer forward failed: %v", err)
	}

	wfResp, ok := res.(*protocol.GetWorkflowResponse)
	if !ok || wfResp.Output == nil || wfResp.Output.WorkflowUUID != "live-wf-42" {
		t.Fatalf("unexpected live workflow response: %+v", res)
	}

	// Failover: expire lease in database and run Node 2 adoption
	_, err = pool.Exec(ctx, "UPDATE executors SET lease_expires_at = now() - interval '10 seconds' WHERE executor_id = $1", execID)
	if err != nil {
		t.Fatalf("failed to expire lease in database: %v", err)
	}

	haMgr2 := ha.NewManager(s.Queries(), ha.ManagerOptions{
		InstanceID:       node2ID,
		AdvertiseAddress: s2Host,
		Port:             s2Port,
		AdoptionInterval: 10 * time.Millisecond,
		LeaseDuration:    30 * time.Second,
	})
	if err := haMgr2.Start(ctx); err != nil {
		t.Fatalf("haMgr2.Start failed: %v", err)
	}
	defer haMgr2.Stop()

	// Wait for Node 2 to adopt the executor lease in PostgreSQL
	adoptDeadline := time.Now().Add(5 * time.Second)
	var adoptedRow gen.Executor
	for time.Now().Before(adoptDeadline) {
		adoptedRow, err = s.Queries().GetExecutorByID(ctx, gen.GetExecutorByIDParams{
			ApplicationID: app.ID,
			ExecutorID:    execID,
		})
		if err == nil && adoptedRow.OwnerInstanceID == node2ID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if adoptedRow.OwnerInstanceID != node2ID {
		t.Errorf("expected lease adopted by node2 (%v) in database, got %v", node2ID, adoptedRow.OwnerInstanceID)
	}
}
