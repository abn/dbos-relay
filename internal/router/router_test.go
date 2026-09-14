package router_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	"github.com/abn/relay/internal/store/gen"
)

type mockStore struct {
	getOrgFunc func(ctx context.Context, name string) (gen.Organisation, error)
	getAppFunc func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error)
}

func (m *mockStore) GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error) {
	if m.getOrgFunc != nil {
		return m.getOrgFunc(ctx, name)
	}
	return gen.Organisation{}, errors.New("unexpected call to GetOrganisationByName")
}

func (m *mockStore) GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
	if m.getAppFunc != nil {
		return m.getAppFunc(ctx, arg)
	}
	return gen.Application{}, errors.New("unexpected call to GetApplicationByName")
}

type mockDispatcher struct {
	dispatchFunc func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

func (m *mockDispatcher) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	if m.dispatchFunc != nil {
		return m.dispatchFunc(ctx, appID, msg)
	}
	return nil, errors.New("unexpected call to Dispatch")
}

func testUUID(b byte) pgtype.UUID {
	var id pgtype.UUID
	id.Bytes[0] = b
	id.Valid = true
	return id
}

func TestDispatch_Success(t *testing.T) {
	ctx := context.Background()
	orgID := testUUID(1)
	appID := testUUID(2)

	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-1"}
	resMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-1"}

	store := &mockStore{
		getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
			if name != "my-org" {
				t.Errorf("expected org name 'my-org', got %q", name)
			}
			return gen.Organisation{ID: orgID, Name: name}, nil
		},
		getAppFunc: func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
			if arg.OrganisationID != orgID {
				t.Errorf("expected org ID %v, got %v", orgID, arg.OrganisationID)
			}
			if arg.Name != "my-app" {
				t.Errorf("expected app name 'my-app', got %q", arg.Name)
			}
			return gen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
		},
	}

	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			if id != appID {
				t.Errorf("expected app ID %v, got %v", appID, id)
			}
			if msg != reqMsg {
				t.Errorf("expected request message %v, got %v", reqMsg, msg)
			}
			return resMsg, nil
		},
	}

	r := router.New(store, dispatcher)
	got, err := r.Dispatch(ctx, "my-org", "my-app", reqMsg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != resMsg {
		t.Errorf("expected response %v, got %v", resMsg, got)
	}
}

func TestDispatch_DefaultLocalOrg(t *testing.T) {
	ctx := context.Background()
	orgID := testUUID(1)
	appID := testUUID(2)
	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-local"}
	resMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-local"}

	calledOrg := ""
	store := &mockStore{
		getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
			calledOrg = name
			return gen.Organisation{ID: orgID, Name: name}, nil
		},
		getAppFunc: func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
			return gen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
		},
	}

	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return resMsg, nil
		},
	}

	r := router.New(store, dispatcher)
	_, err := r.Dispatch(ctx, "", "app-1", reqMsg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calledOrg != "local" {
		t.Errorf("expected default org 'local', got %q", calledOrg)
	}
}

func TestDispatch_OrgNotFound(t *testing.T) {
	ctx := context.Background()
	store := &mockStore{
		getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
			return gen.Organisation{}, pgx.ErrNoRows
		},
	}
	dispatcher := &mockDispatcher{}

	r := router.New(store, dispatcher)
	reqMsg := &protocol.Envelope{RequestID: "req-1"}
	_, err := r.Dispatch(ctx, "nonexistent-org", "app", reqMsg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, router.ErrOrgNotFound) {
		t.Errorf("expected ErrOrgNotFound, got %v", err)
	}
}

func TestDispatch_AppNotFound(t *testing.T) {
	ctx := context.Background()
	orgID := testUUID(1)
	store := &mockStore{
		getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
			return gen.Organisation{ID: orgID, Name: name}, nil
		},
		getAppFunc: func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
			return gen.Application{}, pgx.ErrNoRows
		},
	}
	dispatcher := &mockDispatcher{}

	r := router.New(store, dispatcher)
	reqMsg := &protocol.Envelope{RequestID: "req-1"}
	_, err := r.Dispatch(ctx, "my-org", "nonexistent-app", reqMsg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, router.ErrAppNotFound) {
		t.Errorf("expected ErrAppNotFound, got %v", err)
	}
}

func TestDispatch_StoreUnavailable(t *testing.T) {
	ctx := context.Background()
	connErr := errors.New("read: connection reset by peer")
	store := &mockStore{
		getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
			return gen.Organisation{}, connErr
		},
	}
	dispatcher := &mockDispatcher{}

	r := router.New(store, dispatcher)
	reqMsg := &protocol.Envelope{RequestID: "req-1"}
	_, err := r.Dispatch(ctx, "my-org", "app", reqMsg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, router.ErrStoreUnavailable) {
		t.Errorf("expected ErrStoreUnavailable, got %v", err)
	}
	if !errors.Is(err, connErr) {
		t.Errorf("expected original error in chain, got %v", err)
	}
}

func TestDispatch_NoLiveExecutor(t *testing.T) {
	orgID := testUUID(1)
	appID := testUUID(2)

	cases := []struct {
		name     string
		errReply error
	}{
		{
			name:     "contains no live executor",
			errReply: errors.New("no live executor connected"),
		},
		{
			name:     "contains no executors registered",
			errReply: errors.New("no executors registered for application"),
		},
		{
			name:     "contains no executors available",
			errReply: errors.New("no executors available for application"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &mockStore{
				getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
					return gen.Organisation{ID: orgID, Name: name}, nil
				},
				getAppFunc: func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
					return gen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
				},
			}
			dispatcher := &mockDispatcher{
				dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
					return nil, tc.errReply
				},
			}

			r := router.New(store, dispatcher)
			reqMsg := &protocol.Envelope{RequestID: "req-1"}
			_, err := r.Dispatch(context.Background(), "my-org", "my-app", reqMsg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, router.ErrNoLiveExecutor) {
				t.Errorf("expected ErrNoLiveExecutor, got %v", err)
			}
		})
	}
}

func TestDispatch_ExecutorTimeout(t *testing.T) {
	orgID := testUUID(1)
	appID := testUUID(2)

	cases := []struct {
		name     string
		errReply error
	}{
		{
			name:     "context deadline exceeded",
			errReply: context.DeadlineExceeded,
		},
		{
			name:     "request timed out message",
			errReply: errors.New("request timed out"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &mockStore{
				getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
					return gen.Organisation{ID: orgID, Name: name}, nil
				},
				getAppFunc: func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
					return gen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
				},
			}
			dispatcher := &mockDispatcher{
				dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
					return nil, tc.errReply
				},
			}

			r := router.New(store, dispatcher)
			reqMsg := &protocol.Envelope{RequestID: "req-1"}
			_, err := r.Dispatch(context.Background(), "my-org", "my-app", reqMsg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, router.ErrExecutorTimeout) {
				t.Errorf("expected ErrExecutorTimeout, got %v", err)
			}
		})
	}
}

func TestDispatch_UnclassifiedDispatcherError(t *testing.T) {
	ctx := context.Background()
	orgID := testUUID(1)
	appID := testUUID(2)

	expectedErr := errors.New("underlying network failure")

	store := &mockStore{
		getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
			return gen.Organisation{ID: orgID, Name: name}, nil
		},
		getAppFunc: func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
			return gen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
		},
	}
	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, expectedErr
		},
	}

	r := router.New(store, dispatcher)
	reqMsg := &protocol.Envelope{RequestID: "req-1"}
	_, err := r.Dispatch(ctx, "my-org", "my-app", reqMsg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

type mockDataPlaneManager struct {
	hasDataPlane bool
	onDispatch   func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

func (m *mockDataPlaneManager) HasDataPlane(appID pgtype.UUID) bool {
	return m.hasDataPlane
}

func (m *mockDataPlaneManager) Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
	if m.onDispatch != nil {
		return m.onDispatch(ctx, appID, msg)
	}
	return nil, nil
}

func TestDispatch_DataPlaneFallbackErrors(t *testing.T) {
	ctx := context.Background()
	orgID := testUUID(1)
	appID := testUUID(2)

	store := &mockStore{
		getOrgFunc: func(ctx context.Context, name string) (gen.Organisation, error) {
			return gen.Organisation{ID: orgID, Name: name}, nil
		},
		getAppFunc: func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
			return gen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
		},
	}

	// Dispatcher returns ErrNoLiveExecutor so fallback is triggered
	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, router.ErrNoLiveExecutor
		},
	}

	reqMsg := &protocol.Envelope{RequestID: "req-fallback"}

	// 1. Unsupported operation maps to ErrNoLiveExecutor (yielding 503)
	dpUnsupported := &mockDataPlaneManager{
		hasDataPlane: true,
		onDispatch: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, dataplane.ErrUnsupportedOperation
		},
	}
	r1 := router.New(store, dispatcher)
	r1.SetDataPlane(dpUnsupported)
	_, err := r1.Dispatch(ctx, "my-org", "my-app", reqMsg)
	if !errors.Is(err, router.ErrNoLiveExecutor) {
		t.Fatalf("expected ErrNoLiveExecutor on dataplane.ErrUnsupportedOperation, got: %v", err)
	}

	// 2. Timeout maps to ErrExecutorTimeout
	dpTimeout := &mockDataPlaneManager{
		hasDataPlane: true,
		onDispatch: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, context.DeadlineExceeded
		},
	}
	r2 := router.New(store, dispatcher)
	r2.SetDataPlane(dpTimeout)
	_, err = r2.Dispatch(ctx, "my-org", "my-app", reqMsg)
	if !errors.Is(err, router.ErrExecutorTimeout) {
		t.Fatalf("expected ErrExecutorTimeout on context.DeadlineExceeded, got: %v", err)
	}

	// 3. Connection refusal / other failure maps to ErrDataPlaneUnavailable
	dpRefused := &mockDataPlaneManager{
		hasDataPlane: true,
		onDispatch: func(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")
		},
	}
	r3 := router.New(store, dispatcher)
	r3.SetDataPlane(dpRefused)
	_, err = r3.Dispatch(ctx, "my-org", "my-app", reqMsg)
	if !errors.Is(err, router.ErrDataPlaneUnavailable) {
		t.Fatalf("expected ErrDataPlaneUnavailable on connection refusal, got: %v", err)
	}
}

type trackingStore struct {
	getOrgFunc       func(ctx context.Context, name string) (gen.Organisation, error)
	getAppFunc       func(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error)
	listExecsFunc    func(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error)
	getInstanceFunc  func(ctx context.Context, id pgtype.UUID) (gen.Instance, error)
	getInstanceCalls int
}

func (t *trackingStore) GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error) {
	if t.getOrgFunc != nil {
		return t.getOrgFunc(ctx, name)
	}
	return gen.Organisation{ID: testUUID(1), Name: name}, nil
}

func (t *trackingStore) GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
	if t.getAppFunc != nil {
		return t.getAppFunc(ctx, arg)
	}
	return gen.Application{ID: testUUID(2), OrganisationID: arg.OrganisationID, Name: arg.Name}, nil
}

func (t *trackingStore) ListConnectedExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]gen.Executor, error) {
	if t.listExecsFunc != nil {
		return t.listExecsFunc(ctx, appID)
	}
	return nil, nil
}

func (t *trackingStore) GetInstance(ctx context.Context, id pgtype.UUID) (gen.Instance, error) {
	t.getInstanceCalls++
	if t.getInstanceFunc != nil {
		return t.getInstanceFunc(ctx, id)
	}
	return gen.Instance{
		ID:               id,
		AdvertiseAddress: "127.0.0.1",
		Port:             8090,
		HeartbeatAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}, nil
}

type recordingPeerForwarder struct {
	forwardCalls    int
	recordedTargets []string
	forwardFunc     func(ctx context.Context, targetURL string, msg protocol.Message) (protocol.Message, error)
}

func (rf *recordingPeerForwarder) Forward(ctx context.Context, targetURL string, msg protocol.Message) (protocol.Message, error) {
	rf.forwardCalls++
	rf.recordedTargets = append(rf.recordedTargets, targetURL)
	if rf.forwardFunc != nil {
		return rf.forwardFunc(ctx, targetURL, msg)
	}
	return nil, errors.New("peer forward failed")
}

// TestRouter_PeerForwarding_DeduplicationAndFleetScale tests 5,000 connected executors owned by 2 peers.
func TestRouter_PeerForwarding_DeduplicationAndFleetScale(t *testing.T) {
	ctx := context.Background()
	localInstID := testUUID(1)
	peer1 := testUUID(2)
	peer2 := testUUID(3)
	appID := testUUID(4)

	// Create 5,000 executors split between peer1 and peer2
	executors := make([]gen.Executor, 5000)
	for i := range executors {
		owner := peer1
		if i%2 == 1 {
			owner = peer2
		}
		executors[i] = gen.Executor{
			ApplicationID:   appID,
			ExecutorID:      fmt.Sprintf("exec-%d", i),
			Status:          "connected",
			OwnerInstanceID: owner,
			LeaseExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
		}
	}

	store := &trackingStore{
		listExecsFunc: func(ctx context.Context, id pgtype.UUID) ([]gen.Executor, error) {
			return executors, nil
		},
	}

	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, errors.New("no live executor connected")
		},
	}

	forwarder := &recordingPeerForwarder{}

	r := router.New(store, dispatcher)
	r.SetForwarder(forwarder, localInstID)

	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-fleet"}
	_, err := r.Dispatch(ctx, "my-org", "my-app", reqMsg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Must deduplicate by owner instance: at most 1 GetInstance and 1 Forward per distinct owner (2 total)
	if store.getInstanceCalls > 2 {
		t.Errorf("expected at most 2 GetInstance calls, got %d", store.getInstanceCalls)
	}
	if forwarder.forwardCalls > 2 {
		t.Errorf("expected at most 2 Forward calls, got %d", forwarder.forwardCalls)
	}
}

// TestRouter_PeerForwarding_UnreachableInstanceBounded tests 3 executors on 1 unreachable instance.
func TestRouter_PeerForwarding_UnreachableInstanceBounded(t *testing.T) {
	ctx := context.Background()
	localInstID := testUUID(1)
	peer1 := testUUID(2)
	appID := testUUID(3)

	executors := make([]gen.Executor, 3)
	for i := range executors {
		executors[i] = gen.Executor{
			ApplicationID:   appID,
			ExecutorID:      fmt.Sprintf("exec-%d", i),
			Status:          "connected",
			OwnerInstanceID: peer1,
			LeaseExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
		}
	}

	store := &trackingStore{
		listExecsFunc: func(ctx context.Context, id pgtype.UUID) ([]gen.Executor, error) {
			return executors, nil
		},
	}

	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, errors.New("no live executor connected")
		},
	}

	forwarder := &recordingPeerForwarder{
		forwardFunc: func(ctx context.Context, targetURL string, msg protocol.Message) (protocol.Message, error) {
			time.Sleep(10 * time.Millisecond)
			return nil, errors.New("dial tcp: connection refused")
		},
	}

	r := router.New(store, dispatcher)
	r.SetForwarder(forwarder, localInstID)

	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-unreachable"}
	start := time.Now()
	_, err := r.Dispatch(ctx, "my-org", "my-app", reqMsg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Exactly 1 forward attempt and 1 GetInstance call
	if store.getInstanceCalls != 1 {
		t.Errorf("expected exactly 1 GetInstance call, got %d", store.getInstanceCalls)
	}
	if forwarder.forwardCalls != 1 {
		t.Errorf("expected exactly 1 Forward call, got %d", forwarder.forwardCalls)
	}
	if elapsed > 1*time.Second {
		t.Errorf("expected dispatch to return within configured budget, took %v", elapsed)
	}
}

// TestRouter_PeerForwarding_StaleHeartbeatSkipped tests that an instance with stale heartbeat is skipped.
func TestRouter_PeerForwarding_StaleHeartbeatSkipped(t *testing.T) {
	ctx := context.Background()
	localInstID := testUUID(1)
	stalePeer := testUUID(2)
	appID := testUUID(3)

	store := &trackingStore{
		listExecsFunc: func(ctx context.Context, id pgtype.UUID) ([]gen.Executor, error) {
			return []gen.Executor{
				{
					ApplicationID:   appID,
					ExecutorID:      "exec-stale",
					Status:          "connected",
					OwnerInstanceID: stalePeer,
					LeaseExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
				},
			}, nil
		},
		getInstanceFunc: func(ctx context.Context, id pgtype.UUID) (gen.Instance, error) {
			return gen.Instance{
				ID:               id,
				AdvertiseAddress: "127.0.0.1",
				Port:             8090,
				// Heartbeat older than 2 minutes
				HeartbeatAt: pgtype.Timestamptz{Time: time.Now().Add(-5 * time.Minute), Valid: true},
			}, nil
		},
	}

	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, errors.New("no live executor connected")
		},
	}

	forwarder := &recordingPeerForwarder{}

	r := router.New(store, dispatcher)
	r.SetForwarder(forwarder, localInstID)

	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-stale"}
	_, err := r.Dispatch(ctx, "my-org", "my-app", reqMsg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if forwarder.forwardCalls != 0 {
		t.Errorf("expected 0 forward calls for stale heartbeat peer, got %d", forwarder.forwardCalls)
	}
}

// TestRouter_PeerForwarding_CancelledContext tests that a cancelled context terminates immediately.
func TestRouter_PeerForwarding_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before dispatch

	localInstID := testUUID(1)
	peer1 := testUUID(2)
	appID := testUUID(3)

	store := &trackingStore{
		listExecsFunc: func(ctx context.Context, id pgtype.UUID) ([]gen.Executor, error) {
			return []gen.Executor{
				{
					ApplicationID:   appID,
					ExecutorID:      "exec-cancelled",
					Status:          "connected",
					OwnerInstanceID: peer1,
					LeaseExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
				},
			}, nil
		},
	}

	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, errors.New("no live executor connected")
		},
	}

	forwarder := &recordingPeerForwarder{}

	r := router.New(store, dispatcher)
	r.SetForwarder(forwarder, localInstID)

	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-cancelled"}
	_, err := r.Dispatch(ctx, "my-org", "my-app", reqMsg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if forwarder.forwardCalls != 0 {
		t.Errorf("expected 0 forward calls for cancelled context, got %d", forwarder.forwardCalls)
	}
}

func TestRouter_PeerForwarding_SchemeSupport(t *testing.T) {
	ctx := context.Background()
	localInstID := testUUID(1)
	peer1 := testUUID(2)
	appID := testUUID(3)

	store := &trackingStore{
		listExecsFunc: func(ctx context.Context, id pgtype.UUID) ([]gen.Executor, error) {
			return []gen.Executor{
				{
					ApplicationID:   appID,
					ExecutorID:      "exec-tls",
					Status:          "connected",
					OwnerInstanceID: peer1,
					LeaseExpiresAt:  pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
				},
			}, nil
		},
		getInstanceFunc: func(ctx context.Context, id pgtype.UUID) (gen.Instance, error) {
			return gen.Instance{
				ID:               peer1,
				AdvertiseAddress: "https://peer-tls.example.internal",
				Port:             8090,
				HeartbeatAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
			}, nil
		},
	}

	dispatcher := &mockDispatcher{
		dispatchFunc: func(ctx context.Context, id pgtype.UUID, msg protocol.Message) (protocol.Message, error) {
			return nil, errors.New("no live executor connected")
		},
	}

	forwarder := &recordingPeerForwarder{}

	r := router.New(store, dispatcher)
	r.SetForwarder(forwarder, localInstID)

	reqMsg := &protocol.Envelope{Type: protocol.MessageTypeListWorkflows, RequestID: "req-scheme"}
	_, _ = r.Dispatch(ctx, "my-org", "my-app", reqMsg)

	if len(forwarder.recordedTargets) == 0 {
		t.Fatal("expected at least 1 forward target recorded")
	}
	target := forwarder.recordedTargets[0]
	if !strings.HasPrefix(target, "https://") {
		t.Fatalf("expected forward target to use https scheme, got %s", target)
	}
}
