package chaos_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/fakeexecutor"
	"github.com/abn/relay/internal/hub"
	"github.com/abn/relay/internal/liveness"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
)

type memoryStore struct {
	mu        sync.Mutex
	orgs      map[string]gen.Organisation
	apps      map[string]gen.Application
	appsByID  map[pgtype.UUID]gen.Application
	keys      map[string]gen.ApiKey
	executors map[string]gen.Executor
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		orgs:      make(map[string]gen.Organisation),
		apps:      make(map[string]gen.Application),
		appsByID:  make(map[pgtype.UUID]gen.Application),
		keys:      make(map[string]gen.ApiKey),
		executors: make(map[string]gen.Executor),
	}
}

func (m *memoryStore) GetApplicationByID(ctx context.Context, id pgtype.UUID) (gen.Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app, ok := m.appsByID[id]
	if !ok {
		return gen.Application{}, errors.New("app not found")
	}
	return app, nil
}

func (m *memoryStore) DisconnectExecutor(ctx context.Context, arg gen.DisconnectExecutorParams) (gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.executors[arg.ExecutorID]
	if !ok {
		return gen.Executor{}, errors.New("executor not found")
	}
	e.Status = "disconnected"
	m.executors[arg.ExecutorID] = e
	return e, nil
}

func (m *memoryStore) SetExecutorDead(ctx context.Context, arg gen.SetExecutorDeadParams) (gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.executors[arg.ExecutorID]
	if !ok {
		return gen.Executor{}, errors.New("executor not found")
	}
	e.Status = "dead"
	m.executors[arg.ExecutorID] = e
	return e, nil
}

func (m *memoryStore) DeleteExecutor(ctx context.Context, arg gen.DeleteExecutorParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.executors, arg.ExecutorID)
	return nil
}

func (m *memoryStore) UpsertExecutor(ctx context.Context, arg gen.UpsertExecutorParams) (gen.Executor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := gen.Executor{
		ApplicationID:      arg.ApplicationID,
		ExecutorID:         arg.ExecutorID,
		ApplicationVersion: arg.ApplicationVersion,
		Hostname:           arg.Hostname,
		Status:             "connected",
	}
	m.executors[arg.ExecutorID] = e
	return e, nil
}

func (m *memoryStore) GetAPIKeyByLookup(ctx context.Context, lookup string) (gen.ApiKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.keys[lookup]
	if !ok {
		return gen.ApiKey{}, errors.New("key not found")
	}
	return k, nil
}

func (m *memoryStore) GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app, ok := m.apps[arg.Name]
	if !ok {
		return gen.Application{}, errors.New("app not found")
	}
	return app, nil
}

func (m *memoryStore) GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	org, ok := m.orgs[name]
	if !ok {
		return gen.Organisation{}, errors.New("org not found")
	}
	return org, nil
}

func (m *memoryStore) CreateApplication(ctx context.Context, arg gen.CreateApplicationParams) (gen.Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	app := gen.Application{
		ID:             pgtype.UUID{Bytes: [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, Valid: true},
		OrganisationID: arg.OrganisationID,
		Name:           arg.Name,
		Settings:       arg.Settings,
	}
	m.apps[arg.Name] = app
	m.appsByID[app.ID] = app
	return app, nil
}

func (m *memoryStore) seedTestData() (string, pgtype.UUID) {
	orgID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	appID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	m.orgs["acme"] = gen.Organisation{ID: orgID, Name: "acme"}
	m.apps["test-app"] = gen.Application{ID: appID, OrganisationID: orgID, Name: "test-app"}
	m.appsByID[appID] = m.apps["test-app"]

	rawKey, keyRec, _ := auth.Mint()
	m.keys[keyRec.Lookup] = gen.ApiKey{
		ID:               pgtype.UUID{Bytes: [16]byte{3}, Valid: true},
		OrganisationID:   orgID,
		Name:             "test-key",
		Lookup:           keyRec.Lookup,
		KeyHash:          keyRec.Hash,
		ApplicationNames: []string{"test-app"},
	}

	return rawKey, appID
}

func TestChaos_ExecutorFailureAndWorkflowRecovery(t *testing.T) {
	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newMemoryStore()
	apiKey, appID := store.seedTestData()

	cfg := &config.Config{
		ExecutorDeadline: 5 * time.Second,
	}
	h := hub.New(store, cfg, nil)
	defer func() { _ = h.Close() }()

	dispatcher := liveness.NewRecoveryDispatcher(h, h, store, liveness.DispatcherOptions{})
	manager := liveness.NewManager(clock, store, dispatcher, nil)
	h.SetLivenessTracker(manager)
	defer manager.Stop()

	// Hub Handler
	mux := http.NewServeMux()
	mux.HandleFunc("/websocket/", func(w http.ResponseWriter, r *http.Request) {
		pathParts := r.URL.Path[len("/websocket/"):]
		parts := strings.SplitN(pathParts, "/", 2)
		if len(parts) != 2 {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		_, conductorKey := parts[0], parts[1]
		rec, err := store.GetAPIKeyByLookup(r.Context(), auth.Lookup(conductorKey))
		if err != nil || !auth.Verify(conductorKey, rec.KeyHash) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		h.ServeHTTP(w, r)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Connect Executor 1 (victim)
	exec1 := fakeexecutor.New(fakeexecutor.Options{
		URL:                wsURL,
		AppName:            "test-app",
		ConductorKey:       apiKey,
		ExecutorID:         "exec-victim-1",
		ApplicationVersion: "v1.0.0",
	})
	if err := exec1.Connect(ctx); err != nil {
		t.Fatalf("failed to connect exec1: %v", err)
	}
	go func() { _ = exec1.Run(ctx) }()

	// 2. Connect Executor 2 (healthy survivor)
	exec2 := fakeexecutor.New(fakeexecutor.Options{
		URL:                wsURL,
		AppName:            "test-app",
		ConductorKey:       apiKey,
		ExecutorID:         "exec-survivor-2",
		ApplicationVersion: "v1.0.0",
	})
	if err := exec2.Connect(ctx); err != nil {
		t.Fatalf("failed to connect exec2: %v", err)
	}

	recoveryReceived := make(chan []string, 1)
	exec2.SetHandler(protocol.MessageTypeRecovery, func(m protocol.Message) (protocol.Message, error) {
		req, ok := m.(*protocol.RecoveryRequest)
		if !ok {
			return nil, errors.New("expected RecoveryRequest")
		}
		recoveryReceived <- req.ExecutorIDs
		return &protocol.RecoveryResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeRecovery,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil
	})
	go func() { _ = exec2.Run(ctx) }()

	// Give handshakes a few milliseconds to settle
	time.Sleep(50 * time.Millisecond)

	// Verify both are tracked as connected
	peers, err := h.FindHealthyPeers(ctx, appID)
	if err != nil {
		t.Fatalf("FindHealthyPeers failed: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 connected peers, got %d", len(peers))
	}

	// 3. Chaos: Kill Executor 1 abruptly
	_ = exec1.Close()

	// Give socket close pump a moment to fire unregister
	time.Sleep(50 * time.Millisecond)

	// 4. Advance virtual clock past grace period (61s > 60s default)
	clock.Advance(61 * time.Second)

	// 5. Oracle verification: Survivor executor receives recovery request for dead victim
	select {
	case deadIDs := <-recoveryReceived:
		if len(deadIDs) != 1 || deadIDs[0] != "exec-victim-1" {
			t.Fatalf("expected recovery for 'exec-victim-1', got %v", deadIDs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("oracle timed out waiting for workflow recovery dispatch to survivor")
	}

	// 6. Oracle verification: Dead executor record is deleted from store
	time.Sleep(50 * time.Millisecond)
	store.mu.Lock()
	_, stillExists := store.executors["exec-victim-1"]
	store.mu.Unlock()
	if stillExists {
		t.Fatalf("expected dead executor 'exec-victim-1' record to be deleted after recovery acknowledgment")
	}
}

func TestChaos_CrossTenantRecoveryRefused(t *testing.T) {
	app1 := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	app2 := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	foreignPeers := []liveness.Peer{
		{AppID: app2, ExecutorID: "exec-rogue", ApplicationVersion: "v1"},
	}

	_, err := liveness.SelectCandidates(app1, "dead-1", "v1", foreignPeers, true)
	if !errors.Is(err, liveness.ErrCrossTenantRecovery) {
		t.Fatalf("expected ErrCrossTenantRecovery, got %v", err)
	}
}

func TestChaos_DuplicateRecoveryIsIdempotent(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	ctx := context.Background()

	var deleteCalls int
	deleter := &mockDeleterChaos{onDelete: func() { deleteCalls++ }}

	peers := &mockPeerFinderChaos{
		peers: []liveness.Peer{
			{AppID: appID, ExecutorID: "peer-survivor", ApplicationVersion: "v1"},
		},
	}
	transport := &mockTransportChaos{}

	dispatcher := liveness.NewRecoveryDispatcher(peers, transport, deleter, liveness.DispatcherOptions{})

	// First recovery
	if err := dispatcher.RecoverDeadExecutor(ctx, appID, "dead-id", "v1"); err != nil {
		t.Fatalf("first recovery failed: %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("expected 1 delete call, got %d", deleteCalls)
	}

	// Duplicate recovery (e.g. repeated timer or concurrent sweep)
	if err := dispatcher.RecoverDeadExecutor(ctx, appID, "dead-id", "v1"); err != nil {
		t.Fatalf("duplicate recovery failed: %v", err)
	}
	if deleteCalls != 2 {
		t.Fatalf("expected second delete call, got %d", deleteCalls)
	}
}

type mockDeleterChaos struct {
	onDelete func()
}

func (m *mockDeleterChaos) DeleteExecutor(ctx context.Context, arg gen.DeleteExecutorParams) error {
	if m.onDelete != nil {
		m.onDelete()
	}
	return nil
}

type mockPeerFinderChaos struct {
	peers []liveness.Peer
}

func (m *mockPeerFinderChaos) FindHealthyPeers(ctx context.Context, appID pgtype.UUID) ([]liveness.Peer, error) {
	return m.peers, nil
}

type mockTransportChaos struct{}

func (m *mockTransportChaos) SendRecovery(ctx context.Context, appID pgtype.UUID, targetExecutorID string, req *protocol.RecoveryRequest) (*protocol.RecoveryResponse, error) {
	return &protocol.RecoveryResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeRecovery,
			RequestID: req.RequestID,
		},
		Success: true,
	}, nil
}

func TestChaos_LiveDatabase_ExecutorFailureAndWorkflowRecovery(t *testing.T) {
	dbURL := os.Getenv("RELAY_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("RELAY_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	orgName := fmt.Sprintf("chaos_%d", time.Now().UnixNano()%1000000)
	org, err := s.Queries().CreateOrganisation(ctx, orgName)
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	appName := "test-app"
	app, err := s.Queries().CreateApplication(ctx, gen.CreateApplicationParams{
		OrganisationID: org.ID,
		Name:           appName,
		Settings:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	rawKey, keyRec, err := auth.Mint()
	if err != nil {
		t.Fatalf("failed to mint key: %v", err)
	}

	_, err = s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             "chaos-key",
		Lookup:           keyRec.Lookup,
		KeyHash:          keyRec.Hash,
		ApplicationNames: []string{appName},
		Permissions:      []string{"application.read", "application.write", "websocket.connect"},
	})
	if err != nil {
		t.Fatalf("failed to create api key: %v", err)
	}

	clock := liveness.NewVirtualClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	cfg := &config.Config{
		DatabaseURL:      dbURL,
		ExecutorDeadline: 5 * time.Second,
	}

	h := hub.New(s, cfg, nil)
	defer func() { _ = h.Close() }()

	dispatcher := liveness.NewRecoveryDispatcher(h, h, s.Queries(), liveness.DispatcherOptions{})
	manager := liveness.NewManager(clock, s.Queries(), dispatcher, nil)
	h.SetLivenessTracker(manager)
	defer manager.Stop()

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

	// 1. Connect Executor 1 (victim)
	exec1 := fakeexecutor.New(fakeexecutor.Options{
		URL:                wsURL,
		AppName:            appName,
		ConductorKey:       rawKey,
		ExecutorID:         "exec-victim-1",
		ApplicationVersion: "v1.0.0",
	})
	if err := exec1.Connect(ctx); err != nil {
		t.Fatalf("failed to connect exec1: %v", err)
	}
	go func() { _ = exec1.Run(ctx) }()

	// 2. Connect Executor 2 (healthy survivor)
	exec2 := fakeexecutor.New(fakeexecutor.Options{
		URL:                wsURL,
		AppName:            appName,
		ConductorKey:       rawKey,
		ExecutorID:         "exec-survivor-2",
		ApplicationVersion: "v1.0.0",
	})
	if err := exec2.Connect(ctx); err != nil {
		t.Fatalf("failed to connect exec2: %v", err)
	}

	recoveryReceived := make(chan []string, 1)
	exec2.SetHandler(protocol.MessageTypeRecovery, func(m protocol.Message) (protocol.Message, error) {
		req, ok := m.(*protocol.RecoveryRequest)
		if !ok {
			return nil, errors.New("expected RecoveryRequest")
		}
		recoveryReceived <- req.ExecutorIDs
		return &protocol.RecoveryResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeRecovery,
				RequestID: req.RequestID,
			},
			Success: true,
		}, nil
	})
	go func() { _ = exec2.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)

	peers, err := h.FindHealthyPeers(ctx, app.ID)
	if err != nil {
		t.Fatalf("FindHealthyPeers failed: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 connected peers, got %d", len(peers))
	}

	// 3. Chaos: Kill Executor 1 abruptly
	_ = exec1.Close()
	time.Sleep(50 * time.Millisecond)

	// 4. Advance virtual clock past grace period
	clock.Advance(61 * time.Second)

	// 5. Oracle verification: Survivor receives recovery request
	select {
	case deadIDs := <-recoveryReceived:
		if len(deadIDs) != 1 || deadIDs[0] != "exec-victim-1" {
			t.Fatalf("expected recovery for 'exec-victim-1', got %v", deadIDs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("oracle timed out waiting for workflow recovery dispatch to survivor")
	}

	// 6. Verify in real PostgreSQL store that recovery was executed
	time.Sleep(50 * time.Millisecond)
	execs, err := s.Queries().ListExecutorsByApplication(ctx, app.ID)
	if err != nil {
		t.Fatalf("failed to list executors from DB: %v", err)
	}
	for _, e := range execs {
		if e.ExecutorID == "exec-victim-1" && e.Status == "connected" {
			t.Fatalf("expected exec-victim-1 to not be connected, got %s", e.Status)
		}
	}
}
