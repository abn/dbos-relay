package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/store"
	"github.com/abn/relay/internal/store/gen"
)

func newAPIKeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apikey",
		Short: "Manage Relay API keys",
	}
	cmd.AddCommand(newAPIKeyCreateCommand())
	return cmd
}

func newAPIKeyCreateCommand() *cobra.Command {
	var (
		orgName     string
		keyName     string
		databaseURL string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API key for an organisation",
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

			ctx := cmd.Context()
			org, err := s.Queries().GetOrganisationByName(ctx, orgName)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					org, err = s.Queries().CreateOrganisation(ctx, orgName)
					if err != nil {
						return fmt.Errorf("creating organisation: %w", err)
					}
				} else {
					return fmt.Errorf("getting organisation: %w", err)
				}
			}

			plain, record, err := auth.Mint()
			if err != nil {
				return fmt.Errorf("minting api key: %w", err)
			}

			_, err = s.Queries().CreateAPIKey(ctx, gen.CreateAPIKeyParams{
				OrganisationID:   org.ID,
				Name:             keyName,
				Lookup:           record.Lookup,
				KeyHash:          record.Hash,
				ApplicationNames: []string{},
				Permissions:      []string{},
			})
			if err != nil {
				return fmt.Errorf("storing api key: %w", err)
			}

			cmd.Println(plain)
			return nil
		},
	}

	cmd.Flags().StringVar(&orgName, "org", "", "Organisation name (required)")
	cmd.Flags().StringVar(&keyName, "name", "", "API key name (required)")
	cmd.Flags().StringVar(&databaseURL, "database-url", "", "PostgreSQL database URL (defaults to RELAY_DATABASE_URL env)")

	_ = cmd.MarkFlagRequired("org")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}
