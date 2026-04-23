package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/config"
	"github.com/spacelions/j/internal/workflow"
)

// Execute is the process entry point. It builds the cobra root, parses
// os.Args, and returns the exit code.
func Execute() int {
	root := &cobra.Command{
		Use:   "j",
		Short: "J Harness CLI",
		// Errors are printed once here; don't let cobra print them too.
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return config.Init()
		},
	}
	root.AddCommand(
		&cobra.Command{
			Use:   "run",
			Short: "Run the agent in the ADK console (interactive)",
			RunE: func(*cobra.Command, []string) error {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				return workflow.Run(context.Background(), cfg, nil)
			},
		},
		&cobra.Command{
			Use:   "web",
			Short: "Run the local ADK web server (api + webui) for development",
			Long:  "Starts the ADK web stack. For development and debugging only; not for production.",
			RunE: func(*cobra.Command, []string) error {
				cfg, err := config.Load()
				if err != nil {
					return err
				}
				return workflow.Run(context.Background(), cfg, []string{"web", "api", "webui"})
			},
		},
	)
	root.SetArgs(os.Args[1:])
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "j: %v\n", err)
		return 1
	}
	return 0
}
