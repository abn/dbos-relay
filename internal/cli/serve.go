package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/abn/relay/internal/alerting"
	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/dashboard"
	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/declarative"
	"github.com/abn/relay/internal/ha"
	"github.com/abn/relay/internal/hub"
	"github.com/abn/relay/internal/liveness"
	"github.com/abn/relay/internal/metrics"
	"github.com/abn/relay/internal/router"
	"github.com/abn/relay/internal/safego"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
)

func newServeCommand() *cobra.Command {
	var skipMigrations bool
	var noAuth bool
	var embedded bool
	var databaseURL string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Relay control plane server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !skipMigrations {
				if envVal := os.Getenv("RELAY_SKIP_MIGRATIONS"); envVal == "true" || envVal == "1" {
					skipMigrations = true
				}
			}

			if embedded {
				_ = os.Setenv("RELAY_EMBEDDED", "true")
			}
			if databaseURL != "" {
				_ = os.Setenv("RELAY_DATABASE_URL", databaseURL)
			}

			cfg, err := config.Load(os.Getenv)
			if err != nil {
				return err
			}

			logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
				Level: cfg.LogLevel,
			}))
			slog.SetDefault(logger)

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if cfg.IsEmbedded() {
				dbPath := cfg.DatabaseURL
				if strings.HasPrefix(dbPath, "sqlite://") {
					dbPath = strings.TrimPrefix(dbPath, "sqlite://")
				} else if strings.HasPrefix(dbPath, "sqlite:") {
					dbPath = strings.TrimPrefix(dbPath, "sqlite:")
				}
				if idx := strings.Index(dbPath, "?"); idx != -1 {
					dbPath = dbPath[:idx]
				}
				if dbPath != ":memory:" && !strings.Contains(dbPath, "mode=memory") {
					dir := filepath.Dir(dbPath)
					if dir != "." && dir != "" {
						if err := os.MkdirAll(dir, 0755); err != nil {
							return fmt.Errorf("creating embedded database directory %q: %w", dir, err)
						}
					}
				}
			}

			s, err := store.Open(ctx, cfg.DatabaseURL)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer s.Close()

			if !skipMigrations {
				if err := s.Migrate(ctx); err != nil {
					return fmt.Errorf("migrating database: %w", err)
				}
			}

			h := hub.New(s, cfg, logger)
			defer func() {
				if err := h.Close(); err != nil {
					logger.Error("closing hub", "error", err)
				}
			}()

			livenessMgr := liveness.NewManager(liveness.NewRealClock(), s.Queries(), nil, logger)
			dispatcher := liveness.NewRecoveryDispatcher(h, h, livenessMgr, liveness.DispatcherOptions{Logger: logger})
			dispatcher.SetRecorder(s.Queries())
			livenessMgr.SetRecovery(dispatcher)
			h.SetLivenessTracker(livenessMgr)
			defer livenessMgr.Stop()

			alertEvaluator := alerting.NewEvaluator(s.Queries(), h, logger)
			alertEvaluator.SetChannelDispatcher(alerting.NewHTTPChannelDispatcher(nil))
			stopAlerts := alertEvaluator.Start(ctx, 15*time.Second)
			defer stopAlerts()

			auditSweeper := api.NewAuditRetentionSweeper(s.Queries(), logger)
			stopAuditSweep := auditSweeper.Start(ctx, 0)
			defer stopAuditSweep()

			port := 8090
			if _, pStr, err := net.SplitHostPort(cfg.ListenAddr); err == nil {
				if p, err := strconv.Atoi(pStr); err == nil {
					port = p
				}
			}

			haMgr := ha.NewManager(s.Queries(), ha.ManagerOptions{
				AdvertiseAddress: cfg.AdvertiseAddress,
				Port:             port,
				Logger:           logger,
				Liveness:         livenessMgr,
				Standalone:       cfg.IsEmbedded(),
			})
			if err := haMgr.Start(ctx); err != nil {
				return fmt.Errorf("starting ha manager: %w", err)
			}
			defer haMgr.Stop()

			h.SetInstanceID(haMgr.ID())
			livenessMgr.SetInstanceID(haMgr.ID())

			r := router.New(s.Queries(), h)
			dpManager := dataplane.NewManager(nil)
			defer func() {
				if err := dpManager.Close(); err != nil {
					logger.Error("closing dataplane manager", "error", err)
				}
			}()
			if configPath := os.Getenv("RELAY_CONFIG"); configPath != "" {
				if decCfg, err := declarative.LoadFile(configPath); err == nil {
					if plan, err := declarative.Apply(ctx, s, decCfg); err != nil {
						logger.Warn("failed to apply declarative config", "path", configPath, "error", err)
					} else if plan != nil && len(plan.GeneratedKeys) > 0 {
						logger.Warn("minted default API key during bootstrap; plaintext is not recoverable, set RELAY_API_KEY before startup or run 'relay apikey create'", "keys_count", len(plan.GeneratedKeys))
					}
					allApps, _ := s.Queries().ListAllApplications(ctx)
					appMap := make(map[string]gen.Application)
					for _, a := range allApps {
						appMap[a.Name] = a
					}
					registerDeclarativeDataPlanes(logger, dpManager, decCfg.DataPlanes, appMap)
				} else {
					logger.Warn("failed to load declarative config for data plane", "path", configPath, "error", err)
				}
			}
			r.SetDataPlane(dpManager)

			forwarder := router.NewForwarder([]byte(cfg.InternalSecret), nil)
			r.SetForwarder(forwarder, haMgr.ID())

			apiServer := api.NewServer(r, s.Queries(), logger)
			if !noAuth && cfg.AuthEnabled() {
				val := auth.NewOIDCValidator(cfg.OIDCIssuer, cfg.OIDCAudience, nil)
				if err := val.Init(ctx); err != nil {
					return fmt.Errorf("failed to initialize OIDC validator: %w", err)
				}
				apiServer.WithAuth(true, val)
			} else {
				apiServer.WithAuth(false, nil)
			}
			handler := api.NewHandler(s, apiServer)

			forwardHandler := router.NewForwardHandler(h, []byte(cfg.InternalSecret), 30*time.Second)

			dashHandler := dashboard.Handler(handler)

			mux := http.NewServeMux()
			mux.Handle("/", dashHandler)
			mux.Handle("/websocket/", h)
			mux.Handle("/internal/v1/forward/", forwardHandler)
			metricsHandler := metrics.NewHandlerWithDispatcher(s.Queries(), h)
			metricsHandler.SetLogger(logger)
			mux.Handle("/v1/metrics", api.AuthMiddleware(apiServer)(metricsHandler))

			loggedHandler := loggingMiddleware(logger, mux)

			server := &http.Server{
				Addr:              cfg.ListenAddr,
				Handler:           loggedHandler,
				ReadHeaderTimeout: 10 * time.Second,
			}

			serverErr := make(chan error, 1)
			safego.Go(logger, "http-server", func() {
				if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
					if err := server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
						serverErr <- err
					}
				} else {
					if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
						serverErr <- err
					}
				}
				close(serverErr)
			})

			select {
			case err := <-serverErr:
				if err != nil {
					return fmt.Errorf("http server: %w", err)
				}
				return nil
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := server.Shutdown(shutdownCtx); err != nil {
					return fmt.Errorf("server shutdown: %w", err)
				}
				return nil
			}
		},
	}

	cmd.Flags().BoolVar(&skipMigrations, "skip-migrations", false, "Skip automatic database schema migrations on startup (defaults to RELAY_SKIP_MIGRATIONS env)")
	cmd.Flags().BoolVar(&noAuth, "no-auth", false, "Disable authentication (overrides RELAY_AUTH_ENABLED)")
	cmd.Flags().BoolVarP(&embedded, "embedded", "e", false, "Run in embedded SQLite mode (defaults to ./data/relay.db)")
	cmd.Flags().StringVar(&databaseURL, "database-url", "", "Database connection URL (PostgreSQL or SQLite)")
	return cmd
}

type requestIDContextKey struct{}

var contextKeyRequestID = requestIDContextKey{}

type responseWriterRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (r *responseWriterRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseWriterRecorder) Write(b []byte) (int, error) {
	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytesWritten += n
	return n, err
}

func (r *responseWriterRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := r.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("http.Hijacker not implemented")
}

func (r *responseWriterRecorder) Flush() {
	if fl, ok := r.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-Id")
		if reqID == "" {
			reqID = uuid.New().String()
		}
		w.Header().Set("X-Request-Id", reqID)

		start := time.Now()
		rec := &responseWriterRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		ctx := context.WithValue(r.Context(), contextKeyRequestID, reqID)

		next.ServeHTTP(rec, r.WithContext(ctx))

		duration := time.Since(start)
		logger.Info("http request",
			"method", r.Method,
			"path", redactLogPath(r.URL.Path),
			"status", rec.statusCode,
			"duration", duration,
			"bytes", rec.bytesWritten,
			"request_id", reqID,
		)
	})
}

func redactLogPath(path string) string {
	if strings.HasPrefix(path, "/websocket/") {
		parts := strings.Split(path, "/")
		if len(parts) >= 4 {
			return "/websocket/" + parts[2] + "/[REDACTED]"
		}
	}
	return path
}

func registerDeclarativeDataPlanes(logger *slog.Logger, dpManager dataplane.Manager, dataPlanes map[string]declarative.DataPlane, appMap map[string]gen.Application) {
	for appName, dp := range dataPlanes {
		app, ok := appMap[appName]
		if !ok {
			continue
		}
		dbURL, err := dp.ResolveConnectionURL()
		if err != nil {
			logger.Warn("failed to resolve data plane connection URL", "app", appName, "error", err)
			continue
		}
		timeout := time.Duration(dp.StatementTimeoutSecs) * time.Second
		if err := dpManager.RegisterApp(dataplane.AppConfig{
			ApplicationID:    app.ID,
			ApplicationName:  app.Name,
			DatabaseURL:      dbURL,
			Mode:             dataplane.Mode(dp.Mode),
			StatementTimeout: timeout,
			MaxConnections:   dp.MaxConnections,
		}); err != nil {
			logger.Warn("failed to register data plane", "app", appName, "error", err)
		} else {
			logger.Info("registered data plane for app", "app", appName)
		}
	}
}
