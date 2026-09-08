package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/config"
	"github.com/abn/relay/internal/store"
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

			handler := api.NewHandler(s)
			server := &http.Server{
				Addr:              cfg.ListenAddr,
				Handler:           handler,
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
