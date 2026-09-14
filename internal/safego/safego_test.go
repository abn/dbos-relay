package safego

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type captureHandler struct {
	slog.Handler
	logged chan struct{}
}

func (h *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.Handler.Handle(ctx, r)
	select {
	case h.logged <- struct{}{}:
	default:
	}
	return err
}

func TestSafegoRecoversPanic(t *testing.T) {
	var buf bytes.Buffer
	baseHandler := slog.NewJSONHandler(&buf, nil)
	logged := make(chan struct{}, 1)
	logger := slog.New(&captureHandler{Handler: baseHandler, logged: logged})

	Go(logger, "test-worker", func() {
		panic("simulated worker failure")
	})

	select {
	case <-logged:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for panic log to be emitted")
	}

	out := buf.String()
	if !strings.Contains(out, "panic in test-worker") {
		t.Fatalf("expected log to contain panic message, got: %s", out)
	}
	if !strings.Contains(out, "simulated worker failure") {
		t.Fatalf("expected log to contain panic reason, got: %s", out)
	}
	if !strings.Contains(out, "stack") {
		t.Fatalf("expected log to contain stack trace, got: %s", out)
	}
}
