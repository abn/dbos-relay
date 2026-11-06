package liveness_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/liveness"
	"github.com/abn/relay/internal/store/gen"
)

type fakeProbeStore struct {
	org      gen.Organisation
	app      gen.Application
	upserted []gen.UpsertExecutorParams
}

func (f *fakeProbeStore) GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error) {
	if name != f.org.Name {
		return gen.Organisation{}, errNotFound
	}
	return f.org, nil
}

func (f *fakeProbeStore) GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error) {
	if arg.Name != f.app.Name {
		return gen.Application{}, errNotFound
	}
	return f.app, nil
}

func (f *fakeProbeStore) UpsertExecutor(ctx context.Context, arg gen.UpsertExecutorParams) (gen.Executor, error) {
	f.upserted = append(f.upserted, arg)
	return gen.Executor{ApplicationID: arg.ApplicationID, ExecutorID: arg.ExecutorID}, nil
}

var errNotFound = &notFoundError{}

type notFoundError struct{}

func (e *notFoundError) Error() string { return "not found" }

type fakeProbeSink struct {
	connects    []string
	disconnects []string
}

func (f *fakeProbeSink) OnProbeConnect(ctx context.Context, appID pgtype.UUID, executorID, version string) error {
	f.connects = append(f.connects, executorID)
	return nil
}

func (f *fakeProbeSink) OnProbeDisconnect(ctx context.Context, appID pgtype.UUID, executorID string) {
	f.disconnects = append(f.disconnects, executorID)
}

func TestProbeHandler_HeartbeatAndStop(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	orgID := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	store := &fakeProbeStore{
		org: gen.Organisation{ID: orgID, Name: "default"},
		app: gen.Application{ID: appID, Name: "billing", OrganisationID: orgID},
	}
	sink := &fakeProbeSink{}
	registry := liveness.NewProbeRegistry(sink, nil)
	handler := liveness.NewProbeHandler(store, sink, registry, nil, "secret", "default", pgtype.UUID{}, nil)

	body, _ := json.Marshal(map[string]any{
		"application":         "billing",
		"executor_id":         "exec-1",
		"application_version": "v1",
		"ttl_seconds":         30,
	})
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/probe", bytes.NewReader(body))
	req.Header.Set("X-Relay-Internal-Secret", "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.upserted) != 1 || store.upserted[0].ExecutorID != "exec-1" {
		t.Fatalf("expected executor upsert, got %+v", store.upserted)
	}
	if len(sink.connects) != 1 || sink.connects[0] != "exec-1" {
		t.Fatalf("expected connect, got %+v", sink.connects)
	}

	stopBody, _ := json.Marshal(map[string]any{
		"application": "billing",
		"executor_id": "exec-1",
		"stop":        true,
	})
	stopReq := httptest.NewRequest(http.MethodPost, "/internal/v1/probe", bytes.NewReader(stopBody))
	stopReq.Header.Set("X-Relay-Internal-Secret", "secret")
	stopRec := httptest.NewRecorder()
	handler.ServeHTTP(stopRec, stopReq)
	if stopRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on stop, got %d", stopRec.Code)
	}
	if len(sink.disconnects) != 1 || sink.disconnects[0] != "exec-1" {
		t.Fatalf("expected disconnect, got %+v", sink.disconnects)
	}
}

func TestProbeHandler_RejectsBadSecret(t *testing.T) {
	store := &fakeProbeStore{}
	sink := &fakeProbeSink{}
	handler := liveness.NewProbeHandler(store, sink, liveness.NewProbeRegistry(sink, nil), nil, "secret", "default", pgtype.UUID{}, nil)
	body, _ := json.Marshal(map[string]any{"application": "billing", "executor_id": "exec-1"})
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/probe", bytes.NewReader(body))
	req.Header.Set("X-Relay-Internal-Secret", "wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestProbeRegistry_SweepExpires(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{5}, Valid: true}
	registry := liveness.NewProbeRegistry(&fakeProbeSink{}, nil)
	registry.Touch(appID, "exec-1", time.Minute)
	if expired := registry.Sweep(time.Now()); len(expired) != 0 {
		t.Fatalf("expected no expired entries, got %d", len(expired))
	}
	expired := registry.Sweep(time.Now().Add(2 * time.Minute))
	if len(expired) != 1 {
		t.Fatalf("expected one expired entry, got %d", len(expired))
	}
}

type fakeProbePolicy struct{ allow bool }

func (p fakeProbePolicy) AllowProbe(appID pgtype.UUID) bool { return p.allow }

func TestProbeHandler_RejectsNonDirectApplication(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{6}, Valid: true}
	orgID := pgtype.UUID{Bytes: [16]byte{7}, Valid: true}
	store := &fakeProbeStore{
		org: gen.Organisation{ID: orgID, Name: "default"},
		app: gen.Application{ID: appID, Name: "billing", OrganisationID: orgID},
	}
	sink := &fakeProbeSink{}
	registry := liveness.NewProbeRegistry(sink, nil)
	handler := liveness.NewProbeHandler(store, sink, registry, fakeProbePolicy{allow: false}, "secret", "default", pgtype.UUID{}, nil)
	body, _ := json.Marshal(map[string]any{"application": "billing", "executor_id": "exec-1"})
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/probe", bytes.NewReader(body))
	req.Header.Set("X-Relay-Internal-Secret", "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if len(sink.connects) != 0 {
		t.Fatalf("non-Direct app must not receive a probe connect: %+v", sink.connects)
	}
}

func TestProbeHandler_EmptySecretRejects(t *testing.T) {
	store := &fakeProbeStore{}
	sink := &fakeProbeSink{}
	handler := liveness.NewProbeHandler(store, sink, liveness.NewProbeRegistry(sink, nil), nil, "", "default", pgtype.UUID{}, nil)
	body, _ := json.Marshal(map[string]any{"application": "billing", "executor_id": "exec-1"})
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/probe", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a configured secret, got %d", rec.Code)
	}
}
