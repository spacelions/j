package tasks

import "github.com/spf13/cobra"

// newRedoWorkCmd builds the `j tasks work` cobra subcommand. It
// re-enters the work phase on an existing task: when the row's
// WorkResumeCursor is non-empty the call is forwarded to
// `work.RunResume`; otherwise `work.Run` is invoked with TaskID set
// so plan.md is re-used. Tool/model precedence in the re-run branch
// is `task.WorkTool|WorkModel` -> worker bucket -> prompt; the
// bucket is never written by this command.
func newRedoWorkCmd() *cobra.Command {
	return newRedoCmd(redoCmdSpec{
		phase: redoPhaseWork,
		use:   "work",
		short: "Re-enter the work phase on an existing task (resume if possible, otherwise re-run)",
		long: "Picks a task (via --from-task or the shared picker) and re-enters its work " +
			"phase. When WorkResumeCursor is non-empty the call is forwarded to " +
			"`j work resume`; otherwise the worker is re-run against the existing " +
			"plan.md, preferring the per-phase tool/model recorded on the row " +
			"(falling back to the worker bucket and finally a prompt). The bucket " +
			"interactive value is never persisted; --interactive (or " +
			"TASKS_WORK_INTERACTIVE) is a one-off override that defaults to true.",
		viperKey: "tasks.work",
		envKey:   "TASKS_WORK",
	})
}
