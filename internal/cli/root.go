package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/plan"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/cursor"
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

	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "Generate a plan.md from a markdown task description using a coding agent",
		Long: "Reads a markdown task description, asks which coding agent and model to use, " +
			"runs that agent in plan mode, and writes the resulting plan.md beside the input.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return plan.Run(cmd.Context(), plan.Options{
				Target:      viper.GetString("plan.target"),
				Interactive: viper.GetBool("plan.interactive"),
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      []codingagents.Agent{cursor.New()},
			})
		},
	}
	planCmd.Flags().StringP("target", "t", "", "Path to a markdown file describing the task")
	planCmd.Flags().Bool("interactive", true, "Launch the coding agent in interactive mode (its TUI). Set to false for headless capture.")
	if err := viper.BindPFlag("plan.target", planCmd.Flags().Lookup("target")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "j: bind plan.target: %v\n", err)
		return 1
	}
	if err := viper.BindPFlag("plan.interactive", planCmd.Flags().Lookup("interactive")); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "j: bind plan.interactive: %v\n", err)
		return 1
	}
	if err := viper.BindEnv("plan.target", "PLAN_TARGET"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "j: bind PLAN_TARGET: %v\n", err)
		return 1
	}
	if err := viper.BindEnv("plan.interactive", "PLAN_INTERACTIVE"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "j: bind PLAN_INTERACTIVE: %v\n", err)
		return 1
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
		planCmd,
	)
	root.SetArgs(os.Args[1:])
	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "j: %v\n", err)
		return 1
	}
	return 0
}
