package tasks

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

// newCodeReviewCmd builds the `j tasks code-review` cobra subcommand.
// The same command serves both the user-facing entry point and the
// hidden child invocation: the parent path runs the picker + spawn
// flow; the child path runs the long-running round driver when
// --run-round is passed.
func newCodeReviewCmd() *cobra.Command {
	agents := defaultAgents()
	cmd := &cobra.Command{
		Use:   cmdCodeReview,
		Short: "Run the code-review planner against a task's stored PR",
		Long: "Resolves a task (picker filtered to rows with " +
			"`PullRequestURL`, or `--from-task <id>`), validates that " +
			"the PR is open and accessible on GitHub, fetches the " +
			"conversation comments and review threads, allocates the " +
			"next `code-reviews/round-N/` directory, and asks the " +
			"planner-selected coding agent to decide on each item. " +
			"Default mode runs the long-running sequence as a detached " +
			"background child whose stdout/stderr append to the per-task " +
			"`agent.log`; pass `--interactive` to run it foreground.",
		Args:              cobra.NoArgs,
		PersistentPreRunE: preflight.PreRunE,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			if viper.GetBool("tasks.code_review.run_round") {
				return nil
			}
			// Code-review only invokes the planner bucket; an expired
			// or unconfigured worker/verifier should not block a
			// reviewer who has a usable planner.
			return preflight.EnsurePlannerSelection(
				cmd.Context(),
				preflight.AgentCheckOptions{
					Stdin:  cmd.InOrStdin(),
					Stdout: cmd.OutOrStdout(),
					Stderr: cmd.ErrOrStderr(),
					Agents: agents,
				})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCodeReviewCmd(cmd, agents)
		},
	}
	bindCodeReviewFlags(cmd)
	return cmd
}

func runCodeReviewCmd(
	cmd *cobra.Command, agents []codingagents.Agent,
) error {
	interactive := viper.GetBool("tasks.code_review.interactive")
	if viper.GetBool("tasks.code_review.run_round") {
		return RunCodeReviewChild(cmd.Context(), CodeReviewChildOptions{
			TaskID:      viper.GetString("tasks.code_review.from_task"),
			Interactive: interactive,
			Stdin:       cmd.InOrStdin(),
			Stdout:      cmd.OutOrStdout(),
			Stderr:      cmd.ErrOrStderr(),
			Agents:      agents,
		})
	}
	return RunCodeReview(cmd.Context(), CodeReviewOptions{
		FromTask:    viper.GetString("tasks.code_review.from_task"),
		Interactive: interactive,
		Stdin:       cmd.InOrStdin(),
		Stdout:      cmd.OutOrStdout(),
		Stderr:      cmd.ErrOrStderr(),
		Agents:      agents,
	})
}

func bindCodeReviewFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagKeyFromTask, "",
		"Existing task id whose stored PR to review")
	cmd.Flags().Bool(flagKeyInteractive, false,
		"Run the code-review round in the foreground (no detach)")
	cmd.Flags().Bool(flagKeyRunRound, false,
		"Internal: run the round driver inside the hidden child")
	_ = cmd.Flags().MarkHidden(flagKeyRunRound)
	bindFlagEnv(cmd,
		bindEnv("tasks.code_review.from_task", flagKeyFromTask,
			"TASKS_CODE_REVIEW_FROM_TASK"),
		bindEnv("tasks.code_review.interactive", flagKeyInteractive,
			"TASKS_CODE_REVIEW_INTERACTIVE"),
		bindEnv("tasks.code_review.run_round", flagKeyRunRound,
			"TASKS_CODE_REVIEW_RUN_ROUND"),
	)
}
