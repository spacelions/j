package work

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
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
			"selected in this order: --from-task <id> (load .j/tasks/<id>/plan.md), " +
			"the most recent plan-done task in bbolt, or an " +
			"interactive picker over every task in bbolt. The worker updates the existing task " +
			"row in place (plan-done -> working -> work-done|help). Tasks whose status falls outside " +
			"plan-done / help trigger a yes/no confirm prompt before the worker runs; pass " +
			"--yes/-y (or WORK_YES) to skip it. Pass --tool / --model (or " +
			"WORK_TOOL / WORK_MODEL) for a one-off override that does not update the worker " +
			"bucket; run `j settings reset worker.tool` and/or `j settings reset worker.model` " +
			"to be re-prompted on the next run.",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// resolver.Interactive applies the precedence (explicit
			// flag or env var > stored bucket > cobra default true).
			var explicit *bool
			if cmd.Flags().Changed("interactive") || os.Getenv("WORK_INTERACTIVE") != "" {
				v := viper.GetBool("work.interactive")
				explicit = &v
			}
			return Run(cmd.Context(), Options{
				TaskID:      viper.GetString("work.from_task"),
				Yes:         viper.GetBool("work.yes"),
				Interactive: resolver.Interactive(nil, cmd.ErrOrStderr(), store.BucketWorker, explicit),
				Tool:        viper.GetString("work.tool"),
				Model:       viper.GetString("work.model"),
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("from-task", "", "Existing task id to work on (loads <cwd>/.j/tasks/<id>/plan.md)")
	cmd.Flags().BoolP("yes", "y", false, "Skip the status-mismatch confirmation prompt and run anyway")
	cmd.Flags().Bool("interactive", true, "Launch the coding agent in interactive mode (its TUI). Set to false for headless capture.")
	cmd.Flags().String("tool", "", "Coding agent tool (cursor|claude). One-off override; does not update worker.tool.")
	cmd.Flags().String("model", "", "Model identifier. One-off override; does not update worker.model.")
	_ = viper.BindPFlag("work.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindPFlag("work.yes", cmd.Flags().Lookup("yes"))
	_ = viper.BindPFlag("work.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindPFlag("work.tool", cmd.Flags().Lookup("tool"))
	_ = viper.BindPFlag("work.model", cmd.Flags().Lookup("model"))
	_ = viper.BindEnv("work.from_task", "WORK_FROM_TASK")
	_ = viper.BindEnv("work.yes", "WORK_YES")
	_ = viper.BindEnv("work.interactive", "WORK_INTERACTIVE")
	_ = viper.BindEnv("work.tool", "WORK_TOOL")
	_ = viper.BindEnv("work.model", "WORK_MODEL")
	cmd.AddCommand(newResumeCmd())
	return cmd
}
