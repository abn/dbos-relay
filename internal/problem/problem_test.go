package problem_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abn/relay/internal/problem"
)

func TestWriteSetsContentTypeAndStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	problem.Write(rec, &problem.Problem{
		Title:  "no healthy executor",
		Status: http.StatusServiceUnavailable,
		Detail: "application acme has no connected executor",
	})

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Errorf("status = %d, want %d", got, want)
	}
	if got, want := rec.Header().Get("Content-Type"), "application/problem+json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
}

func TestWriteOmitsEmptyFields(t *testing.T) {
	rec := httptest.NewRecorder()
	problem.Write(rec, &problem.Problem{Title: "bad request", Status: 400})

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	for _, key := range []string{"detail", "instance"} {
		if _, present := body[key]; present {
			t.Errorf("empty %q should be omitted, got %v", key, body[key])
		}
	}
}

func TestProblemRoundTrip(t *testing.T) {
	rec := httptest.NewRecorder()
	problem.Write(rec, &problem.Problem{
		Type:     "about:blank",
		Title:    "validation failed",
		Status:   422,
		Detail:   "invalid name",
		Instance: "/v2/orgs/invalid",
	})

	var got problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Title != "validation failed" || got.Status != 422 || got.Detail != "invalid name" || got.Instance != "/v2/orgs/invalid" {
		t.Fatalf("problem did not round-trip correctly: %+v", got)
	}
}
