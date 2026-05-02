// Package run provides the cobra builder for the `j run` subcommand,
// which launches the ADK console interactively.
package run

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/workflow"
)

// New returns the `j run` cobra subcommand.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the agent in the ADK console (interactive)",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := workflow.LoadConfig()
			if err != nil {
				return err
			}
			return workflow.Run(context.Background(), cfg, nil)
		},
	}
}
