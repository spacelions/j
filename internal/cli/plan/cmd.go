package plan

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
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
		Short: "Plan a task from a markdown description and store it under .j/tasks/<id>/",
		Long: "Reads a markdown task description (via --from-file/-f or PLAN_FROM_FILE), asks " +
			"which coding agent and model to use, runs that agent in plan mode, and stores the " +
			"refined requirements.md and the produced plan.md inside <cwd>/.j/tasks/<id>/. " +
			"No file is written to the workspace; use `j tasks` to list runs and `j work --from-task <id>` " +
			"to execute the plan.",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// We do not construct a *store.Store here: Run's
			// settings helpers open `<cwd>/.j/settings` only for
			// the duration of each individual read/write so the
			// bbolt file lock is never held across agent.Plan.
			// Tests inject their own Store to keep on-disk side
			// effects hermetic; that path stays a fast in-memory
			// no-open shortcut inside the helpers.
			//
			// Only forward Interactive when the user was explicit
			// (cobra flag or env var). When unset we leave it nil
			// so Run can fall back to the stored value or the
			// cobra default true.
			var interactive *bool
			if cmd.Flags().Changed("interactive") || os.Getenv("PLAN_INTERACTIVE") != "" {
				v := viper.GetBool("plan.interactive")
				interactive = &v
			}
			return Run(cmd.Context(), Options{
				FromFile:     viper.GetString("plan.from_file"),
				Interactive:  interactive,
				FromSettings: viper.GetBool("plan.from_settings"),
				Stdin:        cmd.InOrStdin(),
				Stdout:       cmd.OutOrStdout(),
				Stderr:       cmd.ErrOrStderr(),
				Agents:       []codingagents.Agent{cursor.New()},
			})
		},
	}
	cmd.Flags().StringP("from-file", "f", "", "Path to a markdown file describing the task")
	cmd.Flags().Bool("interactive", true, "Launch the coding agent in interactive mode (its TUI). Set to false for headless capture.")
	cmd.Flags().Bool("from-settings", true, "Use the tool/model recorded in <cwd>/.j/settings; pass --from-settings=false to be prompted instead.")
	_ = viper.BindPFlag("plan.from_file", cmd.Flags().Lookup("from-file"))
	_ = viper.BindPFlag("plan.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindPFlag("plan.from_settings", cmd.Flags().Lookup("from-settings"))
	_ = viper.BindEnv("plan.from_file", "PLAN_FROM_FILE")
	_ = viper.BindEnv("plan.interactive", "PLAN_INTERACTIVE")
	_ = viper.BindEnv("plan.from_settings", "PLAN_FROM_SETTINGS")
	cmd.AddCommand(newResumeCmd())
	return cmd
}
