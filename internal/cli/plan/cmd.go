package plan

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/cursor"
)

// New returns the `j plan` cobra subcommand. It owns its own flag and
// viper bindings so callers (cli.Execute) only need to register it on
// the root command.
//
// viper.BindPFlag and viper.BindEnv only fail when their input is nil
// or empty — programmer errors that this function does not produce —
// so their returned errors are intentionally discarded.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Generate a plan.md from a markdown task description using a coding agent",
		Long: "Reads a markdown task description, asks which coding agent and model to use, " +
			"runs that agent in plan mode, and writes the resulting plan.md beside the input.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return Run(cmd.Context(), Options{
				Target:      viper.GetString("plan.target"),
				Interactive: viper.GetBool("plan.interactive"),
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      []codingagents.Agent{cursor.New()},
			})
		},
	}
	cmd.Flags().StringP("target", "t", "", "Path to a markdown file describing the task")
	cmd.Flags().Bool("interactive", true, "Launch the coding agent in interactive mode (its TUI). Set to false for headless capture.")
	_ = viper.BindPFlag("plan.target", cmd.Flags().Lookup("target"))
	_ = viper.BindPFlag("plan.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindEnv("plan.target", "PLAN_TARGET")
	_ = viper.BindEnv("plan.interactive", "PLAN_INTERACTIVE")
	return cmd
}
