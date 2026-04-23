package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/adkapp"
	"github.com/spacelions/j/internal/config"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Run the local ADK web server (api + webui) for development",
	Long:  "Starts the ADK web stack. For development and debugging only; not for production.",
	RunE: func(_ *cobra.Command, _ []string) error {
		ctx := context.Background()
		key := config.GoogleAPIKey()
		if key == "" {
			return fmt.Errorf("GOOGLE_API_KEY is not set: set the environment variable, use direnv with .envrc, or pass --google-api-key")
		}
		return adkapp.Run(ctx, key, []string{"web", "api", "webui"})
	},
}
