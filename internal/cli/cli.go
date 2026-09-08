// Package cli assembles the relay command tree.
package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// Version is set at build time by the linker.
var Version = "dev"

// Run executes the command tree with the given arguments.
func Run(ctx context.Context, args []string) error {
	cmd := newRootCommand()
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:          "relay",
		Short:        "Relay control plane server for DBOS Transact applications",
		SilenceUsage: true,
	}

	root.AddCommand(newVersionCommand())
	return root
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the build version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println(Version)
		},
	}
}
