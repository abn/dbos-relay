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

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply database schema migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := databaseURL
			if url == "" {
				url = os.Getenv("RELAY_DATABASE_URL")
			}
			if url == "" {
				return errors.New("database URL is required (set --database-url or RELAY_DATABASE_URL)")
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

	cmd.Flags().StringVar(&databaseURL, "database-url", "", "PostgreSQL database URL (defaults to RELAY_DATABASE_URL env)")

	return cmd
}
