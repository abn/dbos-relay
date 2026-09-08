package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

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
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
)

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Relay control plane server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(os.Getenv)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			s, err := store.Open(ctx, cfg.DatabaseURL)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer s.Close()

			if err := s.Migrate(ctx); err != nil {
				return fmt.Errorf("migrating database: %w", err)
			}

			logger := slog.Default()
			h := hub.New(s, cfg, logger)
			defer func() {
				if err := h.Close(); err != nil {
					logger.Error("closing hub", "error", err)
				}
			}()

			dispatcher := liveness.NewRecoveryDispatcher(h, h, s.Queries(), liveness.DispatcherOptions{Logger: logger})
			livenessMgr := liveness.NewManager(liveness.NewRealClock(), s.Queries(), dispatcher, logger)
			h.SetLivenessTracker(livenessMgr)
			defer livenessMgr.Stop()

			alertEvaluator := alerting.NewEvaluator(s.Queries(), h, logger)
			alertEvaluator.SetChannelDispatcher(alerting.NewHTTPChannelDispatcher(nil))
			stopAlerts := alertEvaluator.Start(ctx, 15*time.Second)
			defer stopAlerts()

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
			})
			if err := haMgr.Start(ctx); err != nil {
				return fmt.Errorf("starting ha manager: %w", err)
			}
			defer haMgr.Stop()

			r := router.New(s.Queries(), h)
			dpManager := dataplane.NewManager(nil)
			if configPath := os.Getenv("RELAY_CONFIG"); configPath != "" {
				if decCfg, err := declarative.LoadFile(configPath); err == nil {
					if _, err := declarative.Apply(ctx, s, decCfg); err != nil {
						logger.Warn("failed to apply declarative config", "path", configPath, "error", err)
					}
					allApps, _ := s.Queries().ListAllApplications(ctx)
					appMap := make(map[string]gen.Application)
					for _, a := range allApps {
						appMap[a.Name] = a
					}
					for appName, dp := range decCfg.DataPlanes {
						app, ok := appMap[appName]
						if !ok {
							continue
						}
						timeout := time.Duration(dp.StatementTimeoutSecs) * time.Second
						_ = dpManager.RegisterApp(dataplane.AppConfig{
							ApplicationID:    app.ID,
							DatabaseURL:      dp.ConnectionURL,
							Mode:             dataplane.Mode(dp.Mode),
							StatementTimeout: timeout,
							MaxConnections:   dp.MaxConnections,
						})
					}
				} else {
					logger.Warn("failed to load declarative config for data plane", "path", configPath, "error", err)
				}
			}
			r.SetDataPlane(dpManager)

			forwarder := router.NewForwarder([]byte(cfg.InternalSecret), nil)
			r.SetForwarder(forwarder, haMgr.ID())

			apiServer := api.NewServer(r, s.Queries(), logger)
			if cfg.AuthEnabled() {
				val := auth.NewOIDCValidator(cfg.OIDCIssuer, cfg.OIDCAudience, nil)
				apiServer.WithAuth(true, val)
			}
			handler := api.NewHandler(s, apiServer)

			forwardHandler := router.NewForwardHandler(h, []byte(cfg.InternalSecret), 30*time.Second)

			dashHandler := dashboard.Handler(handler)

			mux := http.NewServeMux()
			mux.Handle("/", dashHandler)
			mux.Handle("/websocket/", h)
			mux.Handle("/internal/v1/forward/", forwardHandler)
			mux.Handle("/v1/metrics", metrics.NewHandler(s.Queries()))

			server := &http.Server{
				Addr:              cfg.ListenAddr,
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
			}

			serverErr := make(chan error, 1)
			go func() {
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					serverErr <- err
				}
				close(serverErr)
			}()

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
}
