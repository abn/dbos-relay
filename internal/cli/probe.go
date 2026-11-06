package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

type probePayload struct {
	Organisation       string `json:"organisation,omitempty"`
	Application        string `json:"application"`
	ExecutorID         string `json:"executor_id"`
	ApplicationVersion string `json:"application_version,omitempty"`
	Hostname           string `json:"hostname,omitempty"`
	TTLSeconds         int    `json:"ttl_seconds,omitempty"`
	Stop               bool   `json:"stop,omitempty"`
}

func newProbeCommand() *cobra.Command {
	var (
		relayURL string
		org      string
		app      string
		executor string
		version  string
		hostname string
		secret   string
		interval time.Duration
		ttl      time.Duration
		once     bool
	)

	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Send liveness heartbeats for a Direct mode application",
		Long: "Send liveness heartbeats to Relay without linking a DBOS SDK. " +
			"The probe shares the application process lifetime and reports the executor ID " +
			"Relay uses to detect a dead executor and recover its work.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if app == "" {
				app = os.Getenv("DBOS_APP_NAME")
			}
			if app == "" {
				return errors.New("application name is required (set --app or DBOS_APP_NAME)")
			}
			if executor == "" {
				executor = os.Getenv("DBOS__VMID")
			}
			if executor == "" {
				return errors.New("executor ID is required (set --executor-id or DBOS__VMID)")
			}
			if version == "" {
				version = os.Getenv("DBOS__APPVERSION")
			}
			if hostname == "" {
				hostname, _ = os.Hostname()
			}
			if secret == "" {
				secret = os.Getenv("RELAY_INTERNAL_SECRET")
			}
			if interval <= 0 {
				return errors.New("--interval must be positive")
			}
			if ttl <= interval {
				return errors.New("--ttl must be greater than --interval")
			}
			if relayURL == "" {
				relayURL = "http://127.0.0.1:8090"
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			payload := probePayload{
				Organisation:       org,
				Application:        app,
				ExecutorID:         executor,
				ApplicationVersion: version,
				Hostname:           hostname,
				TTLSeconds:         int(ttl.Seconds()),
			}
			send := func(stop bool) error {
				body := payload
				body.Stop = stop
				return postProbe(ctx, relayURL, secret, body)
			}

			if once {
				return send(false)
			}
			if err := send(false); err != nil {
				return err
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = postProbe(stopCtx, relayURL, secret, probePayload{
						Organisation: org,
						Application:  app,
						ExecutorID:   executor,
						Stop:         true,
					})
					return nil
				case <-ticker.C:
					if err := send(false); err != nil {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "relay probe: %v\n", err)
					}
				}
			}
		},
	}

	cmd.Flags().StringVar(&relayURL, "relay-url", "", "Relay base URL (default http://127.0.0.1:8090)")
	cmd.Flags().StringVar(&org, "org", "default", "Relay organisation name")
	cmd.Flags().StringVar(&app, "app", "", "Application name (default DBOS_APP_NAME)")
	cmd.Flags().StringVar(&executor, "executor-id", "", "Executor ID (default DBOS__VMID)")
	cmd.Flags().StringVar(&version, "version", "", "Application version (default DBOS__APPVERSION)")
	cmd.Flags().StringVar(&hostname, "hostname", "", "Hostname reported to Relay")
	cmd.Flags().StringVar(&secret, "secret", "", "Relay internal secret (default RELAY_INTERNAL_SECRET)")
	cmd.Flags().DurationVar(&interval, "interval", 10*time.Second, "Heartbeat interval")
	cmd.Flags().DurationVar(&ttl, "ttl", 30*time.Second, "Heartbeat expiry before Relay starts the grace period")
	cmd.Flags().BoolVar(&once, "once", false, "Send one heartbeat and exit")
	return cmd
}

func postProbe(ctx context.Context, relayURL, secret string, payload probePayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding probe: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(relayURL, "/")+"/internal/v1/probe", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Relay-Internal-Secret", secret)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("relay probe rejected: %s", resp.Status)
	}
	return nil
}
