package tasks

import "github.com/spf13/cobra"

// newRedoVerifyCmd builds the `j tasks verify` cobra subcommand. It
// re-enters the verify phase on an existing task: when the row's
// VerifyResumeCursor is non-empty the call is forwarded to
// `verify.RunResume`; otherwise `verify.Run` is invoked with TaskID
// set so the original task directory is re-used. Tool/model
// precedence in the re-run branch is `task.VerifyTool|VerifyModel`
// -> verifier bucket -> prompt; the bucket is never written by this
// command.
func newRedoVerifyCmd() *cobra.Command {
	return newRedoCmd(redoCmdSpec{
		phase: redoPhaseVerify,
		use:   "verify",
		short: "Re-enter the verify phase on an existing task (resume if possible, otherwise re-verify)",
		long: "Picks a task (via --from-task or the shared picker) and re-enters its " +
			"verify phase. When VerifyResumeCursor is non-empty the call is forwarded " +
			"to `j verify resume`; otherwise the verifier is re-run against the " +
			"existing task directory, preferring the per-phase tool/model recorded on " +
			"the row (falling back to the verifier bucket and finally a prompt). The " +
			"bucket interactive value is never persisted; --interactive (or " +
			"TASKS_VERIFY_INTERACTIVE) is a one-off override that defaults to true.",
		viperKey: "tasks.verify",
		envKey:   "TASKS_VERIFY",
	})
}
