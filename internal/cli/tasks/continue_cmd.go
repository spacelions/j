package tasks

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
)

// newContinueCmd builds the `j tasks continue` cobra subcommand with
// --from-task, --tool, --model, and --interactive flags. --tool,
// --model, and --interactive are forwarded into worker.Run on the
// plan-done dispatch path; resume phases ignore them.
func newContinueCmd() *cobra.Command {
	agents := defaultAgents()
	cmd := &cobra.Command{
		Use: "continue",
		Short: "Continue a task by dispatching to the right phase" +
			" based on status",
		Long: "Resolves a task (via --from-task or the shared picker) " +
			"and dispatches to the right phase based on its status: " +
			"planning -> resume-plan hint, plan-done -> direct worker " +
			"run, working -> resume-work hint, work-done -> verifier, " +
			"verifying -> inline resume-verify. Already-finished tasks " +
			"(failed, completed) print `J: task <id> already finished` " +
			"and exit 0; a `help` row resumes whichever phase produced " +
			"the failure.",
		PersistentPreRunE: preflight.PreRunE,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return preflight.EnsureAgentSelections(cmd.Context(),
				preflight.AgentCheckOptions{
					Stdin:  cmd.InOrStdin(),
					Stdout: cmd.OutOrStdout(),
					Stderr: cmd.ErrOrStderr(),
					Agents: agents,
				})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			interactive := explicitBoolPtr(cmd, flagKeyInteractive,
				"tasks.continue.interactive",
				"TASKS_CONTINUE_INTERACTIVE")
			return RunContinue(cmd.Context(), ContinueOptions{
				TaskID:      viper.GetString("tasks.continue.from_task"),
				Tool:        viper.GetString("tasks.continue.tool"),
				Model:       viper.GetString("tasks.continue.model"),
				Interactive: interactive,
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      agents,
			})
		},
	}
	bindContinueFlags(cmd)
	return cmd
}

func bindContinueFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String(flagKeyFromTask, "",
		"Continue the named task without showing the picker")
	f.String("tool", "",
		"Coding agent tool for plan-done dispatch (cursor|claude)")
	f.String("model", "", "Model identifier for plan-done dispatch")
	f.Bool(flagKeyInteractive, false,
		"Launch the coding agent in interactive mode on plan-done dispatch")
	bindFlagEnv(cmd,
		bindEnv("tasks.continue.from_task", flagKeyFromTask,
			"TASKS_CONTINUE_FROM_TASK"),
		bindEnv("tasks.continue.tool", "tool", "TASKS_CONTINUE_TOOL"),
		bindEnv("tasks.continue.model", "model", "TASKS_CONTINUE_MODEL"),
		bindEnv("tasks.continue.interactive", flagKeyInteractive,
			"TASKS_CONTINUE_INTERACTIVE"),
	)
}
