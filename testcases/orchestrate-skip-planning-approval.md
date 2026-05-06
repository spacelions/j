Prerequisites:
  - From the project root.

Steps:
  - Run the orchestrate tests that verify SkipPlanning behavior with
    project-level plan_requires_approval:
      go test ./internal/cli/tasks/... \
        -run "TestRunOrchestrate_SkipPlanningIgnoresProjectApproval|TestRunOrchestrate_SkipPlanningSkipWorkIgnoresProjectApproval|TestRunOrchestrate_SkipPlanningConflictExplicitOverrideOnly|TestRunOrchestrate_SkipPlanningExplicitFalseAllowed|TestRunOrchestrate_SkipPlanningRunsWorkVerify|TestRunOrchestrate_SkipPlanningConflictsWithApproval|TestRunOrchestrate_PlanApprovalStopsAfterPlan|TestRunOrchestrate_PassFirstTry" \
        -count=1 -v

Expected:
  - All listed tests pass.
  - TestRunOrchestrate_SkipPlanningIgnoresProjectApproval: project
    plan_requires_approval=true stored, SkipPlanning=true,
    PlanRequiresApproval=nil → worker/verifier both run without error.
  - TestRunOrchestrate_SkipPlanningSkipWorkIgnoresProjectApproval: same
    setup but SkipWork=true → verifier-only path without error.
  - TestRunOrchestrate_SkipPlanningConflictExplicitOverrideOnly:
    project default=true AND explicit requirePlanApproval() +
    SkipPlanning=true → error "skip-planning is incompatible".
  - TestRunOrchestrate_SkipPlanningExplicitFalseAllowed: project
    default=true but explicit noPlanApproval() + SkipPlanning=true →
    proceeds without error.
  - TestRunOrchestrate_SkipPlanningRunsWorkVerify: SkipPlanning with
    noPlanApproval() → planCalls=0, workCalls=1, verifyCalls=1.
  - TestRunOrchestrate_SkipPlanningConflictsWithApproval: explicit
    requirePlanApproval() + SkipPlanning=true → error.
  - TestRunOrchestrate_PlanApprovalStopsAfterPlan: planner path with
    approval gate → plan-done status, work/verify=0.
  - TestRunOrchestrate_PassFirstTry: planner→worker→verifier all run.
