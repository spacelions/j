package tasks

// Cobra subcommand names used when building orchestrate argv.
const (
	cmdOrchestrate = "orchestrate"
	cmdTasks       = "tasks"
	cmdPlan        = "plan"
	cmdCodeReview  = "code-review"
)

// Flag names / values used when building orchestrate argv.
const (
	flagID                       = "--id"
	flagInteractiveTrue          = "--interactive=true"
	flagPlanRequiresApprovalTrue = "--plan-requires-approval=true"
	flagPhasePlanOnly            = "--phase=plan-only"
	flagPhaseFromWork            = "--phase=from-work"
	flagPhaseWorkOnly            = "--phase=work-only"
	flagPhaseVerifyOnly          = "--phase=verify-only"
)

// Cobra flag key names (used with cmd.Flags() calls).
const (
	flagKeyFromTask    = "from-task"
	flagKeyInteractive = "interactive"
	flagKeyModel       = "model"
	flagKeyTool        = "tool"
	flagKeyRunRound    = "run-round"
	flagKeyRound       = "round"
)

// Code-review argv flags assembled when re-execing the hidden child
// process. The literal strings match the flag keys above; pinning
// them here means a future flag rename only has to touch one file.
const (
	flagRunRound           = "--run-round"
	flagCodeReviewFromTask = "--from-task"
)
