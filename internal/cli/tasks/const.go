package tasks

// Cobra subcommand names used when building orchestrate argv.
const (
	cmdOrchestrate = "orchestrate"
	cmdTasks       = "tasks"
	cmdPlan        = "plan"
)

// Flag names / values used when building orchestrate argv.
const (
	flagID                       = "--id"
	flagInteractiveTrue          = "--interactive=true"
	flagPlanRequiresApprovalTrue = "--plan-requires-approval=true"
	flagPhaseFromWork            = "--phase=from-work"
	flagPhaseVerifyOnly          = "--phase=verify-only"
)

// Cobra flag key names (used with cmd.Flags() calls).
const (
	flagKeyFromTask    = "from-task"
	flagKeyInteractive = "interactive"
	flagKeyModel       = "model"
	flagKeyTool        = "tool"
)
