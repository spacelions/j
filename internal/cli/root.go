package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/config"
)

var (
	configFile     string
	googleAPIKeyFL string
)

// Execute runs the root Cobra command and returns a process exit code.
func Execute() int {
	if err := rootCmd.Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "j: %v\n", err)
		return 1
	}
	return 0
}

var rootCmd = &cobra.Command{
	Use:   "j",
	Short: "J agent CLI (ADK, Gemini)",
	// Errors are printed once in Execute.
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		return config.Init(configFile, googleAPIKeyFL)
	},
}

func init() {
	p := rootCmd.PersistentFlags()
	p.StringVar(&configFile, "config", "", "path to config file (YAML, optional if unset at default paths)")
	p.StringVar(&googleAPIKeyFL, "google-api-key", "", "Google / Gemini API key (overrides GOOGLE_API_KEY)")

	rootCmd.AddCommand(runCmd, webCmd)
}
