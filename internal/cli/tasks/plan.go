package tasks

import "github.com/spf13/cobra"

// newRedoPlanCmd builds the `j tasks plan` cobra subcommand. It
// re-enters the plan phase on an existing task: when the row's
// PlanResumeCursor is non-empty the call is forwarded to
// `plan.RunResume`; otherwise `plan.Run` is invoked with TaskID set
// so requirements.md is re-used. Tool/model precedence in the
// re-run branch is `task.PlanTool|PlanModel` -> planner bucket ->
// prompt; the bucket is never written by this command.
func newRedoPlanCmd() *cobra.Command {
	return newRedoCmd(redoCmdSpec{
		phase: redoPhasePlan,
		use:   "plan",
		short: "Re-enter the plan phase on an existing task (resume if possible, otherwise re-plan)",
		long: "Picks a task (via --from-task or the shared picker) and re-enters its plan " +
			"phase. When PlanResumeCursor is non-empty the call is forwarded to " +
			"`j plan resume`; otherwise the planner is re-run against the existing " +
			"requirements.md, preferring the per-phase tool/model recorded on the row " +
			"(falling back to the planner bucket and finally a prompt). The bucket " +
			"interactive value is never persisted; --interactive (or " +
			"TASKS_PLAN_INTERACTIVE) is a one-off override that defaults to true.",
		viperKey: "tasks.plan",
		envKey:   "TASKS_PLAN",
	})
}
