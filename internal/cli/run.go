package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/config"
	"github.com/spacelions/j/internal/workflow"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the agent in the ADK console (interactive)",
	RunE: func(_ *cobra.Command, _ []string) error {
		ctx := context.Background()
		key := config.GoogleAPIKey()
		if key == "" {
			return fmt.Errorf("GOOGLE_API_KEY is not set: set the environment variable, use direnv with .envrc, or pass --google-api-key")
		}
		// default universal launcher: first sublauncher (console) when no args
		return workflow.Run(ctx, key, nil)
	},
}
