package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/abn/relay/internal/store"
)

func newMigrateCommand() *cobra.Command {
	var databaseURL string
	var embedded bool

	resolveURL := func() (string, error) {
		url := databaseURL
		if url == "" {
			url = os.Getenv("RELAY_DATABASE_URL")
		}
		if url == "" && (embedded || os.Getenv("RELAY_EMBEDDED") == "true" || os.Getenv("RELAY_EMBEDDED") == "1") {
			url = "sqlite://./data/relay.db"
		}
		if url == "" {
			return "", errors.New("database URL is required (set --database-url, --embedded, or RELAY_DATABASE_URL)")
		}
		return url, nil
	}

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply database schema migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := resolveURL()
			if err != nil {
				return err
			}

			s, err := store.Open(cmd.Context(), url)
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer s.Close()

			if err := s.Migrate(cmd.Context()); err != nil {
				return fmt.Errorf("running migrations: %w", err)
			}

			cmd.Println("database migrations applied successfully")
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&databaseURL, "database-url", "", "Database URL (defaults to RELAY_DATABASE_URL env)")
	cmd.PersistentFlags().BoolVarP(&embedded, "embedded", "e", false, "Use embedded SQLite database (defaults to ./data/relay.db)")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect the current schema version and dirty flag",
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := resolveURL()
			if err != nil {
				return err
			}

			s, err := store.Open(cmd.Context(), url)
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer s.Close()

			v, dirty, err := s.MigrateVersion(cmd.Context())
			if err != nil {
				return fmt.Errorf("reading migration status: %w", err)
			}

			cmd.Printf("version=%d dirty=%t\n", v, dirty)
			return nil
		},
	}

	var forceVersion int
	var forceConfirm bool
	forceCmd := &cobra.Command{
		Use:   "force",
		Short: "Force the schema version and clear dirty state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !forceConfirm {
				return errors.New("forcing schema version requires --confirm")
			}
			if forceVersion < 0 {
				return errors.New("version must be non-negative")
			}

			url, err := resolveURL()
			if err != nil {
				return err
			}

			s, err := store.Open(cmd.Context(), url)
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer s.Close()

			if err := s.MigrateForce(cmd.Context(), forceVersion); err != nil {
				return fmt.Errorf("forcing schema version: %w", err)
			}

			cmd.Printf("forced schema version to %d and cleared dirty state\n", forceVersion)
			return nil
		},
	}
	forceCmd.Flags().IntVar(&forceVersion, "version", 0, "Target schema version to force")
	forceCmd.Flags().BoolVar(&forceConfirm, "confirm", false, "Confirm forcing schema version")
	_ = forceCmd.MarkFlagRequired("version")

	var downSteps int
	downCmd := &cobra.Command{
		Use:   "down",
		Short: "Roll back schema migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			url, err := resolveURL()
			if err != nil {
				return err
			}

			s, err := store.Open(cmd.Context(), url)
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer s.Close()

			if err := s.MigrateDown(cmd.Context(), downSteps); err != nil {
				return fmt.Errorf("rolling back migrations: %w", err)
			}

			cmd.Printf("rolled back %d migration step(s) successfully\n", downSteps)
			return nil
		},
	}
	downCmd.Flags().IntVar(&downSteps, "steps", 1, "Number of migration steps to roll back")

	upCmd := &cobra.Command{
		Use:   "up",
		Short: "Apply pending database schema migrations",
		RunE:  cmd.RunE,
	}

	cmd.AddCommand(statusCmd, forceCmd, downCmd, upCmd)
	return cmd
}
