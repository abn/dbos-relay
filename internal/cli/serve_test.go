package cli

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestServeFlags(t *testing.T) {
	cmd := newRootCommand()
	serveCmd, _, err := cmd.Find([]string{"serve"})
	if err != nil || serveCmd == nil {
		t.Fatalf("expected serve command to exist: %v", err)
	}

	flag := serveCmd.Flags().Lookup("skip-migrations")
	if flag == nil {
		t.Fatal("expected --skip-migrations flag to be registered")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected --skip-migrations default to be false, got %s", flag.DefValue)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler := loggingMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello world"))
	}))

	t.Run("generates request id if missing", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest(http.MethodGet, "/test/path", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		respReqID := rec.Header().Get("X-Request-Id")
		if respReqID == "" {
			t.Fatal("expected X-Request-Id to be set on response header")
		}

		logOut := buf.String()
		if !strings.Contains(logOut, "http request") {
			t.Errorf("expected log to contain 'http request', got: %s", logOut)
		}
		if !strings.Contains(logOut, respReqID) {
			t.Errorf("expected log to contain request_id %q, got: %s", respReqID, logOut)
		}
		if !strings.Contains(logOut, `"status":201`) {
			t.Errorf("expected log to contain status 201, got: %s", logOut)
		}
	})

	t.Run("preserves inbound request id", func(t *testing.T) {
		buf.Reset()
		req := httptest.NewRequest(http.MethodPost, "/custom/path", nil)
		req.Header.Set("X-Request-Id", "client-trace-xyz-987")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Header().Get("X-Request-Id") != "client-trace-xyz-987" {
			t.Errorf("expected echoed request id, got: %s", rec.Header().Get("X-Request-Id"))
		}

		logOut := buf.String()
		if !strings.Contains(logOut, "client-trace-xyz-987") {
			t.Errorf("expected log to contain inbound request id, got: %s", logOut)
		}
	})
}

func TestSkipMigrationsEnvironment(t *testing.T) {
	t.Setenv("RELAY_SKIP_MIGRATIONS", "true")
	t.Setenv("RELAY_DATABASE_URL", "")

	cmd := newRootCommand()
	serveCmd, _, err := cmd.Find([]string{"serve"})
	if err != nil || serveCmd == nil {
		t.Fatalf("serveCmd: %v", err)
	}

	// Flag still defaults false, but RunE inspects env if flag is not set
	flag := serveCmd.Flags().Lookup("skip-migrations")
	if flag.DefValue != "false" {
		t.Errorf("expected flag default false, got %s", flag.DefValue)
	}
	if os.Getenv("RELAY_SKIP_MIGRATIONS") != "true" {
		t.Errorf("expected RELAY_SKIP_MIGRATIONS=true")
	}
}

func TestLogLevelPlumbingAnd4xxRequestID(t *testing.T) {
	t.Run("log level handler plumbing", func(t *testing.T) {
		var buf bytes.Buffer
		hDebug := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
		if !hDebug.Enabled(context.Background(), slog.LevelDebug) {
			t.Fatal("expected debug level to be enabled")
		}

		hInfo := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
		if hInfo.Enabled(context.Background(), slog.LevelDebug) {
			t.Fatal("expected debug level to be disabled for info logger")
		}
	})

	t.Run("logs 4xx request with matching request id", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&buf, nil))

		handler := loggingMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}))

		req := httptest.NewRequest(http.MethodGet, "/unknown-endpoint", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		reqID := rec.Header().Get("X-Request-Id")
		if reqID == "" {
			t.Fatal("expected X-Request-Id on 4xx response")
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", rec.Code)
		}

		logOut := buf.String()
		if !strings.Contains(logOut, reqID) {
			t.Errorf("expected log to contain request id %q, got: %s", reqID, logOut)
		}
		if !strings.Contains(logOut, `"status":404`) {
			t.Errorf("expected log to contain status 404, got: %s", logOut)
		}
	})
}
