package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/abn/relay/internal/doctor"
)

func newDoctorCommand() *cobra.Command {
	var (
		databaseURL     string
		embedded        bool
		manifestPath    string
		relayURL        string
		probeApp        string
		probeExecutorID string
		timeout         time.Duration
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run read-only migration and production preflight checks",
		Long: "Check the control-plane database, migration and rollback state, " +
			"application system databases, the declarative manifest, and the " +
			"Direct mode probe configuration. Doctor never migrates, writes to an " +
			"application system database, or prints credentials.",
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := resolveDoctorDatabaseURL(databaseURL, embedded)
			if err != nil {
				return doctor.NewUsageError(err)
			}

			report, err := doctor.Run(cmd.Context(), doctor.Options{
				DatabaseURL:     url,
				ManifestPath:    manifestPath,
				RelayURL:        relayURL,
				ProbeApp:        probeApp,
				ProbeExecutorID: probeExecutorID,
				Env:             os.Getenv,
				Timeout:         timeout,
			})
			if err != nil {
				return doctor.NewUsageError(err)
			}

			_, _ = fmt.Fprint(cmd.OutOrStdout(), report.String())
			if failed := report.Failed(); failed > 0 {
				return &doctor.FailureError{Failed: failed}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&databaseURL, "database-url", "", "Control-plane database URL (defaults to RELAY_DATABASE_URL)")
	cmd.Flags().BoolVarP(&embedded, "embedded", "e", false, "Use the embedded SQLite database (defaults to ./data/relay.db)")
	cmd.Flags().StringVarP(&manifestPath, "file", "f", "", "Declarative manifest to check (defaults to RELAY_CONFIG)")
	cmd.Flags().StringVar(&relayURL, "relay-url", "", "Relay base URL for the health check (defaults to RELAY_PROBE_URL, RELAY_URL, or http://127.0.0.1:8090)")
	cmd.Flags().StringVar(&probeApp, "probe-app", "", "Direct application name for the probe identity check (defaults to DBOS_APP_NAME)")
	cmd.Flags().StringVar(&probeExecutorID, "probe-executor-id", "", "Executor ID for the probe identity check (defaults to DBOS__VMID)")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "Timeout for each network operation")
	return cmd
}

func resolveDoctorDatabaseURL(databaseURL string, embedded bool) (string, error) {
	url := databaseURL
	if url == "" {
		url = os.Getenv("RELAY_DATABASE_URL")
	}
	if url == "" && (embedded || truthy(os.Getenv("RELAY_EMBEDDED"))) {
		url = "sqlite://./data/relay.db"
	}
	if url == "" {
		return "", errors.New("database URL is required (set --database-url, --embedded, or RELAY_DATABASE_URL)")
	}
	return url, nil
}

func truthy(value string) bool {
	return value == "true" || value == "1"
}
