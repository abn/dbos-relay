package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/abn/relay/internal/declarative"
	"github.com/abn/relay/internal/store"
)

func newApplyCommand() *cobra.Command {
	var (
		manifestPath string
		databaseURL  string
		envOut       string
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a declarative configuration manifest to Relay",
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestPath == "" {
				return errors.New("manifest file is required (set -f or --file)")
			}

			cfg, err := declarative.LoadFile(manifestPath)
			if err != nil {
				return fmt.Errorf("loading manifest: %w", err)
			}

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

			plan, err := declarative.Apply(cmd.Context(), s, cfg)
			if err != nil {
				return fmt.Errorf("applying manifest: %w", err)
			}

			if envOut != "" && len(plan.GeneratedKeys) > 0 {
				if err := ensureGitIgnoredIfInRepo(cmd.Context(), envOut); err != nil {
					return err
				}
				var lines []string
				for _, v := range plan.GeneratedKeys {
					lines = append(lines, fmt.Sprintf("RELAY_API_KEY=%s\n", v))
				}
				if err := os.WriteFile(envOut, []byte(strings.Join(lines, "")), 0600); err != nil {
					return fmt.Errorf("writing env file %s: %w", envOut, err)
				}
			}

			cmd.Print(plan.String())
			cmd.Println(plan.Summary())
			return nil
		},
	}

	cmd.Flags().StringVarP(&manifestPath, "file", "f", "", "Path to relay.yaml configuration file (required)")
	cmd.Flags().StringVar(&databaseURL, "database-url", "", "PostgreSQL database URL (defaults to RELAY_DATABASE_URL env)")
	cmd.Flags().StringVar(&envOut, "env-out", "", "Optional path to write generated environment variables (e.g. RELAY_API_KEY)")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}

func newDiffCommand() *cobra.Command {
	var (
		manifestPath string
		databaseURL  string
	)

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show differences between declarative manifest and Relay database state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if manifestPath == "" {
				return errors.New("manifest file is required (set -f or --file)")
			}

			cfg, err := declarative.LoadFile(manifestPath)
			if err != nil {
				return fmt.Errorf("loading manifest: %w", err)
			}

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

			plan, err := declarative.Diff(cmd.Context(), s, cfg)
			if err != nil {
				return fmt.Errorf("computing diff: %w", err)
			}

			cmd.Print(plan.String())
			cmd.Println(plan.Summary())
			return nil
		},
	}

	cmd.Flags().StringVarP(&manifestPath, "file", "f", "", "Path to relay.yaml configuration file (required)")
	cmd.Flags().StringVar(&databaseURL, "database-url", "", "PostgreSQL database URL (defaults to RELAY_DATABASE_URL env)")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}

func ensureGitIgnoredIfInRepo(ctx context.Context, path string) error {
	dir := filepath.Dir(path)
	if absDir, err := filepath.Abs(dir); err == nil {
		dir = absDir
	}
	topLevelCmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	topLevelCmd.Dir = dir
	topOut, err := topLevelCmd.Output()
	if err != nil {
		return nil
	}
	topLevel := strings.TrimSpace(string(topOut))
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(topLevel, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil
	}
	checkCmd := exec.CommandContext(ctx, "git", "check-ignore", "-q", absPath)
	checkCmd.Dir = topLevel
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("refusing to write secrets to unignored path %s inside git repository", path)
	}
	return nil
}
