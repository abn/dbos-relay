package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/abn/relay/internal/conformance"
)

func newConformanceCommand() *cobra.Command {
	var (
		targetURL    string
		conductorKey string
		orgName      string
		appName      string
		reportPath       string
		timeout          time.Duration
		allowDestructive bool
	)

	cmd := &cobra.Command{
		Use:   "test-conformance",
		Short: "Run the end-to-end Conductor conformance test suite against a target server",
		Long: `Executes the 8 DBOS Conductor conformance batteries against a target control plane.

Batteries tested:
  1. Specification & System Probes (/healthz, /openapi.json, /docs, /v1/metrics)
  2. WebSocket Handshake & Fleet Registration (/websocket/{app}/{key})
  3. REST & Wire Multiplexing (Workflows, Steps, Observability)
  4. Workflow Control Operations (Cancel, Resume, Restart)
  5. Queues & Schedules Operations (Listing, Pause, Resume)
  6. Workflow Recovery & Liveness Lifecycle
  7. Alerting Rules Management (Create, List, Delete)
  8. RFC 9457 Problem Details & Identity Gating

WARNING: The conformance suite writes state to the target server. It is intended for
scratch or staging environments only. It will register six executors, create an application
row if the app name is new, create an alerting rule in battery 7 (cleaned up automatically),
and route wire mutations to connected executors. Use --allow-destructive to permit running
against non-local targets.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if conductorKey == "" {
				conductorKey = os.Getenv("DBOS_CONDUCTOR_KEY")
			}
			if conductorKey == "" {
				conductorKey = os.Getenv("RELAY_API_KEY")
			}

			isLocal := strings.Contains(targetURL, "localhost") || strings.Contains(targetURL, "127.0.0.1")
			if !isLocal && !allowDestructive {
				return errors.New("refusing to run conformance suite against non-local target without --allow-destructive flag")
			}

			cfg := conformance.Config{
				TargetURL:    targetURL,
				ConductorKey: conductorKey,
				OrgName:      orgName,
				AppName:          appName,
				Output:           cmd.OutOrStdout(),
				Timeout:          timeout,
				AllowDestructive: allowDestructive,
			}

			report, err := conformance.Run(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("conformance run error: %w", err)
			}

			cmd.Println()
			_ = report.FormatMarkdown(cmd.OutOrStdout())

			if reportPath != "" {
				f, err := os.Create(reportPath)
				if err != nil {
					return fmt.Errorf("failed to create report file %s: %w", reportPath, err)
				}
				defer func() { _ = f.Close() }()
				if err := report.FormatMarkdown(f); err != nil {
					return fmt.Errorf("failed to write report to %s: %w", reportPath, err)
				}
				cmd.Printf("\nScorecard report written to %s\n", reportPath)
			}

			if !report.AllPassed {
				return errors.New("conformance suite failed: target is non-conformant")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&targetURL, "target", "http://localhost:8090", "Target Conductor or Relay HTTP base URL")
	cmd.Flags().StringVar(&conductorKey, "key", "", "API key for authentication (or DBOS_CONDUCTOR_KEY / RELAY_API_KEY)")
	cmd.Flags().StringVar(&orgName, "org", "acme", "Organization name for testing")
	cmd.Flags().StringVar(&appName, "app", "conformance-app", "Application name for testing")
	cmd.Flags().StringVar(&reportPath, "report", "", "File path to save the Markdown scorecard report")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Timeout duration per battery")
	cmd.Flags().BoolVar(&allowDestructive, "allow-destructive", false, "Allow destructive operations (creation/deletion) against non-local targets")

	return cmd
}
