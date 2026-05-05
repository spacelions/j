package plan

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
			"to execute the plan. Pass --from-task <id> (or PLAN_FROM_TASK) to re-plan an existing " +
			"task in place. Pass --tool / --model (or PLAN_TOOL / PLAN_MODEL) for a one-off " +
			"override that does not update the planner bucket; run `j settings reset planner.tool` " +
			"and/or `j settings reset planner.model` to be re-prompted on the next run. Pass " +
			"--yes/-y (or PLAN_YES) to skip the status-mismatch confirmation prompt when the " +
			"resolved task is not in plan-done / help.",
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
			// resolver.Interactive applies the precedence (explicit
			// flag or env var > stored bucket > cobra default true).
			// The non-nil pointer signals "user was explicit"; nil
			// falls back to the stored value.
			var explicit *bool
			if cmd.Flags().Changed("interactive") || os.Getenv("PLAN_INTERACTIVE") != "" {
				v := viper.GetBool("plan.interactive")
				explicit = &v
			}
			return Run(cmd.Context(), Options{
				FromFile:    viper.GetString("plan.from_file"),
				FromLinear:  viper.GetString("plan.from_linear"),
				TaskID:      viper.GetString("plan.from_task"),
				Yes:         viper.GetBool("plan.yes"),
				Interactive: resolver.Interactive(nil, cmd.ErrOrStderr(), store.BucketPlanner, explicit),
				Tool:        viper.GetString("plan.tool"),
				Model:       viper.GetString("plan.model"),
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().StringP("from-file", "f", "", "Path to a markdown file describing the task")
	cmd.Flags().String("from-linear", "", "Linear issue identifier (e.g. ENG-123); requires linear.api_key in settings")
	cmd.Flags().String("from-task", "", "Existing task id to re-plan in place (loads <cwd>/.j/tasks/<id>/requirements.md)")
	cmd.Flags().BoolP("yes", "y", false, "Skip the status-mismatch confirmation prompt and re-plan anyway")
	cmd.Flags().Bool("interactive", true, "Launch the coding agent in interactive mode (its TUI). Set to false for headless capture.")
	cmd.Flags().String("tool", "", "Coding agent tool (cursor|claude). One-off override; does not update planner.tool.")
	cmd.Flags().String("model", "", "Model identifier. One-off override; does not update planner.model.")
	_ = viper.BindPFlag("plan.from_file", cmd.Flags().Lookup("from-file"))
	_ = viper.BindPFlag("plan.from_linear", cmd.Flags().Lookup("from-linear"))
	_ = viper.BindPFlag("plan.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindPFlag("plan.yes", cmd.Flags().Lookup("yes"))
	_ = viper.BindPFlag("plan.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindPFlag("plan.tool", cmd.Flags().Lookup("tool"))
	_ = viper.BindPFlag("plan.model", cmd.Flags().Lookup("model"))
	_ = viper.BindEnv("plan.from_file", "PLAN_FROM_FILE")
	_ = viper.BindEnv("plan.from_linear", "PLAN_FROM_LINEAR")
	_ = viper.BindEnv("plan.from_task", "PLAN_FROM_TASK")
	_ = viper.BindEnv("plan.yes", "PLAN_YES")
	_ = viper.BindEnv("plan.interactive", "PLAN_INTERACTIVE")
	_ = viper.BindEnv("plan.tool", "PLAN_TOOL")
	_ = viper.BindEnv("plan.model", "PLAN_MODEL")
	cmd.AddCommand(newResumeCmd())
	return cmd
}
