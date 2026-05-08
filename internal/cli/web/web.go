// Package web provides the cobra builder for the `j web` subcommand,
// which starts the local ADK web stack (api + webui) for development.
package web

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/lifecycle/orchestrator"
	"github.com/spacelions/j/internal/store"
)

// New returns the `j web` cobra subcommand.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "web",
		Short: "Run the local ADK web server (api + webui) for development",
		Long: "Starts the ADK web stack. For development and " +
			"debugging only; not for production.",
		RunE: func(*cobra.Command, []string) error {
			cfg, err := store.LoadProjectConfig()
			if err != nil {
				return err
			}
			return orchestrator.Run(
				context.Background(), cfg,
				[]string{"web", "api", "webui"},
			)
		},
	}
}
