package hub

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abn/relay/internal/protocol"
)

func TestHandleAuthError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{
			name:       "invalid conductor key",
			err:        errors.New("invalid conductor key"),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing app name or conductor key",
			err:        errors.New("missing app name or conductor key"),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "key does not have access to this application",
			err:        errors.New("key does not have access to this application"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "generic error",
			err:        errors.New("some unexpected database error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			HandleAuthError(rec, req, tt.err)
			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestMultiplexer_LateResponse(t *testing.T) {
	m := NewMultiplexer()
	ch, unregister := m.Register("req1")

	// Simulate timeout
	unregister()

	// Late response should not panic
	routed := m.RouteResponse(&protocol.Envelope{RequestID: "req1"})
	if routed {
		t.Error("expected false, got true")
	}

	// Read from channel should yield nothing and be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed")
		}
	default:
		t.Error("expected channel to be closed")
	}
}
