package work

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/cursor"
)

// New returns the `j work` cobra subcommand. It owns its own flag and
// viper bindings so callers (cli.Execute) only need to register it on
// the root command.
//
// viper.BindPFlag and viper.BindEnv only fail when their input is nil
// or empty — programmer errors that this function does not produce —
// so their returned errors are intentionally discarded.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "work",
		Short: "Run a coding agent against an existing plan markdown file",
		Long: "Reads a plan markdown file, asks which coding agent and model to use, " +
			"verifies that backend is signed in, and hands the plan to the agent " +
			"so it can execute the plan against the plan's directory.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return Run(cmd.Context(), Options{
				Target:       viper.GetString("work.target"),
				Interactive:  viper.GetBool("work.interactive"),
				FromSettings: viper.GetBool("work.from_settings"),
				Stdin:        cmd.InOrStdin(),
				Stdout:       cmd.OutOrStdout(),
				Stderr:       cmd.ErrOrStderr(),
				Agents:       []codingagents.Agent{cursor.New()},
			})
		},
	}
	cmd.Flags().StringP("target", "t", "", "Path to a plan markdown file produced by `j plan`")
	cmd.Flags().Bool("interactive", true, "Launch the coding agent in interactive mode (its TUI). Set to false for headless capture.")
	cmd.Flags().Bool("from-settings", true, "Use the tool/model recorded in <cwd>/.j/settings; pass --from-settings=false to be prompted instead.")
	_ = viper.BindPFlag("work.target", cmd.Flags().Lookup("target"))
	_ = viper.BindPFlag("work.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindPFlag("work.from_settings", cmd.Flags().Lookup("from-settings"))
	_ = viper.BindEnv("work.target", "WORK_TARGET")
	_ = viper.BindEnv("work.interactive", "WORK_INTERACTIVE")
	_ = viper.BindEnv("work.from_settings", "WORK_FROM_SETTINGS")
	return cmd
}
