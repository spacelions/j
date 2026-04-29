package work

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
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
		Short: "Run a coding agent against a plan stored under .j/tasks/<id>/",
		Long: "Resolves a plan to execute and hands it to a coding agent. The plan is " +
			"selected in this order: --task <id> (load .j/tasks/<id>/plan.md), " +
			"--from-file/-f or WORK_FROM_FILE (legacy import: copy the file into a fresh " +
			".j/tasks/<new-id>/plan.md), the most recent plan-done task in bbolt, or an " +
			"interactive picker over plan-done tasks. The coder updates the existing task " +
			"row in place (plan-done -> working -> work-done|help) when sourced from " +
			"bbolt; legacy imports create a new task row.",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return Run(cmd.Context(), Options{
				TaskID:       viper.GetString("work.task"),
				FromFile:     viper.GetString("work.from_file"),
				Interactive:  viper.GetBool("work.interactive"),
				FromSettings: viper.GetBool("work.from_settings"),
				Stdin:        cmd.InOrStdin(),
				Stdout:       cmd.OutOrStdout(),
				Stderr:       cmd.ErrOrStderr(),
				Agents:       []codingagents.Agent{cursor.New()},
			})
		},
	}
	cmd.Flags().String("task", "", "Existing task id to work on (loads <cwd>/.j/tasks/<id>/plan.md)")
	cmd.Flags().StringP("from-file", "f", "", "Legacy: import an external plan markdown file into a new task")
	cmd.Flags().Bool("interactive", true, "Launch the coding agent in interactive mode (its TUI). Set to false for headless capture.")
	cmd.Flags().Bool("from-settings", true, "Use the tool/model recorded in <cwd>/.j/settings; pass --from-settings=false to be prompted instead.")
	_ = viper.BindPFlag("work.task", cmd.Flags().Lookup("task"))
	_ = viper.BindPFlag("work.from_file", cmd.Flags().Lookup("from-file"))
	_ = viper.BindPFlag("work.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindPFlag("work.from_settings", cmd.Flags().Lookup("from-settings"))
	_ = viper.BindEnv("work.task", "WORK_TASK")
	_ = viper.BindEnv("work.from_file", "WORK_FROM_FILE")
	_ = viper.BindEnv("work.interactive", "WORK_INTERACTIVE")
	_ = viper.BindEnv("work.from_settings", "WORK_FROM_SETTINGS")
	return cmd
}
