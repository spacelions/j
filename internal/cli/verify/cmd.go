package verify

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
)

// defaultMaxIterations bounds the verifier / worker fix loop. Three
// is the default the plan asks for: enough to converge on small
// follow-up fixes, low enough that a divergent loop fails fast.
const defaultMaxIterations = 3

// New returns the `j verify` cobra subcommand. It owns its own flag
// and viper bindings so callers (cli.Execute) only need to register
// it on the root command, mirroring `j work`'s shape.
//
// viper.BindPFlag and viper.BindEnv only fail when their input is nil
// or empty — programmer errors that this function does not produce —
// so their returned errors are intentionally discarded.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Run a verifier against a work-done task and loop until VERDICT: PASS",
		Long: "Resolves a task and hands it to a verifier agent. The task is selected in " +
			"this order: --from-task <id> (load .j/tasks/<id>/), the most recent work-done " +
			"task in bbolt, or an interactive picker over every task. Tasks whose status " +
			"falls outside work-done / verify-done / help trigger a yes/no confirm prompt " +
			"before the verifier runs; pass --yes/-y (or VERIFY_YES) to skip it. The " +
			"verifier writes verifier_plan.md and verifier_findings.md inside the task " +
			"directory; on VERDICT: FAIL the orchestrator resumes the worker with the " +
			"findings and re-runs the verifier up to --max-iterations times before " +
			"terminating as verify-done. Pass --tool / --model (or VERIFY_TOOL / VERIFY_MODEL) " +
			"for a one-off override that does not update the verifier bucket; run " +
			"`j settings reset verifier.tool` and/or `j settings reset verifier.model` to " +
			"be re-prompted on the next run.",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var interactive *bool
			if cmd.Flags().Changed("interactive") || os.Getenv("VERIFY_INTERACTIVE") != "" {
				v := viper.GetBool("verify.interactive")
				interactive = &v
			}
			return Run(cmd.Context(), Options{
				TaskID:        viper.GetString("verify.from_task"),
				Yes:           viper.GetBool("verify.yes"),
				Interactive:   interactive,
				Tool:          viper.GetString("verify.tool"),
				Model:         viper.GetString("verify.model"),
				MaxIterations: viper.GetInt("verify.max_iterations"),
				Stdin:         cmd.InOrStdin(),
				Stdout:        cmd.OutOrStdout(),
				Stderr:        cmd.ErrOrStderr(),
				Agents:        []codingagents.Agent{cursor.New(), claude.New()},
			})
		},
	}
	cmd.Flags().String("from-task", "", "Existing task id to verify (loads <cwd>/.j/tasks/<id>/)")
	cmd.Flags().BoolP("yes", "y", false, "Skip the status-mismatch confirmation prompt and verify anyway")
	cmd.Flags().Bool("interactive", true, "Launch the verifier agent in interactive mode (its TUI). Set to false for headless capture.")
	cmd.Flags().String("tool", "", "Coding agent tool (cursor|claude). One-off override; does not update verifier.tool.")
	cmd.Flags().String("model", "", "Model identifier. One-off override; does not update verifier.model.")
	cmd.Flags().Int("max-iterations", defaultMaxIterations, "Maximum verifier / worker-fix iterations before terminating as verify-done.")
	_ = viper.BindPFlag("verify.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindPFlag("verify.yes", cmd.Flags().Lookup("yes"))
	_ = viper.BindPFlag("verify.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindPFlag("verify.tool", cmd.Flags().Lookup("tool"))
	_ = viper.BindPFlag("verify.model", cmd.Flags().Lookup("model"))
	_ = viper.BindPFlag("verify.max_iterations", cmd.Flags().Lookup("max-iterations"))
	_ = viper.BindEnv("verify.from_task", "VERIFY_FROM_TASK")
	_ = viper.BindEnv("verify.yes", "VERIFY_YES")
	_ = viper.BindEnv("verify.interactive", "VERIFY_INTERACTIVE")
	_ = viper.BindEnv("verify.tool", "VERIFY_TOOL")
	_ = viper.BindEnv("verify.model", "VERIFY_MODEL")
	_ = viper.BindEnv("verify.max_iterations", "VERIFY_MAX_ITERATIONS")
	cmd.AddCommand(newResumeCmd())
	return cmd
}
