Prerequisites:
  - From the project root.
  - Worktree branch j-fix-j-tasks-re-work-so-it-stops-failing-with-the
    fetched.

Steps:
  1. Check the non-test file line count:
       git show remotes/origin/j-fix-j-tasks-re-work-so-it-stops-failing-with-the:internal/cli/tasks/orchestrate.go | wc -l
  2. Verify the error message is byte-identical to the pre-existing one:
       git show remotes/origin/j-fix-j-tasks-re-work-so-it-stops-failing-with-the:internal/cli/tasks/orchestrate.go | rg -c 'tasks: --skip-planning is incompatible with --plan-requires-approval=true'
  3. Check that no new packages were introduced:
       git diff --name-only 7c11cd6..remotes/origin/j-fix-j-tasks-re-work-so-it-stops-failing-with-the -- '*.go' | rg -v '_test\.go$' | wc -l
  4. Verify cobra flag surface is unchanged:
       go test ./internal/cli/tasks/... -run "TestNewOrchestrateCmd_FlagDefaults|TestNewOrchestrateCmd_FlagsBindToViper|TestNewOrchestrateCmd_EnvBindings|TestNewOrchestrateCmd_SkipPlanningFlagBindings|TestNewOrchestrateCmd_SkipPlanningEnvBinding|TestOrchestratePlanRequiresApprovalOverride_NoFlag|TestOrchestratePlanRequiresApprovalOverride_Env" -count=1 -v
  5. Verify no changes to re_work.go / re_verify.go / resume_work.go /
     resume_verify.go / start.go argv builders:
       git diff --name-only 7c11cd6..remotes/origin/j-fix-j-tasks-re-work-so-it-stops-failing-with-the -- internal/cli/tasks/re_work.go internal/cli/tasks/re_verify.go internal/cli/tasks/resume_work.go internal/cli/tasks/resume_verify.go internal/cli/tasks/start.go

Expected:
  - orchestrate.go is 228 lines (≤300 cap).
  - Error message count is 1 (the single conflict guard).
  - Only 1 non-test file changed (orchestrate.go).
  - All flag-binding and env-binding tests pass.
  - No argv changes in re_* / resume_* / start.go (empty diff).
