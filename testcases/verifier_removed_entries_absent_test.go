package testcases_test

import (
	"strings"
	"testing"
)

// TestAllowlist_RemovedEntriesAbsent verifies that the three symbols
// that were explicitly removed in SPA-77's first batch are no longer
// present in coverage.allowlist, ensuring they cannot be silently
// re-added without first being covered by tests.
func TestAllowlist_RemovedEntriesAbsent(t *testing.T) {
	t.Parallel()
	body := readRepoFile(t, "coverage.allowlist")
	mustBeAbsent := []string{
		`internal/cli/preflight/preflight.go:.*PreRunE`,
		`internal/cli/settings/display.go:.*displayKey`,
		`internal/cli/settings/display.go:.*storageKey`,
		`internal/cli/settings/list.go:.*collectSections`,
		`internal/cli/settings/list.go:.*printSections`,
		`internal/cli/settings/reset.go:.*readConfirmationLine`,
		`internal/cli/settings/reset.go:.*runResetOneKey`,
		`internal/cli/settings/set.go:.*parseBucketKey`,
		`internal/cli/tasks/show.go:.*RunShowClarification`,
		`internal/cli/tasks/show.go:.*newShowClarificationCmd`,
		`internal/cli/tasks/worktree.go:.*removeTaskWorktree`,
		`internal/agents/worker/run.go:.*Run`,
		`internal/agents/worker/run.go:.*RunResume`,
		`internal/agents/worker/run.go:.*listResumableTasks`,
		`internal/agents/worker/run.go:.*resolveResumeTask`,
		`internal/cli/tasks/continue.go:.*resumeFromPlanDone`,
		`internal/cli/tasks/continue.go:.*stampSpawnOnRow`,
		`internal/cli/tasks/continue_dispatch.go:.*replanAsDetachedOrchestrator`,
		`internal/cli/tasks/resume_plan.go:.*resolveResumePlanTaskID`,
		`internal/lifecycle/orchestrator/workflow_task.go:.*driveSequential`,
		`internal/lifecycle/orchestrator/workflow_task.go:.*finaliseVerifyFailIfStuck`,
		`internal/lifecycle/plan.go:.*BeginPlanReuse`,
		`internal/lifecycle/tuiquit/tuiquit.go:.*DecideVerify`,
		`internal/lifecycle/tuiquit/tuiquit.go:.*runGhPRList`,
		`internal/lifecycle/work.go:.*clarificationPresent`,
		`internal/cli/tasks/cmd.go:.*writeTasks`,
		`internal/cli/tasks/orchestrate_cmd.go:.*newOrchestrateCmd`,
		`internal/cli/tasks/resume_plan.go:.*RunResumePlan`,
		`internal/coding-agents/resume.go:.*CaptureAndRecordResume`,
		`internal/coding-agents/cursor/format.go:.*flattenToolCall`,
		`internal/coding-agents/cursor/cursor.go:.*CreateChatID`,
		`internal/coding-agents/deepseek/capture.go:.*decodeSession`,
		`internal/coding-agents/deepseek/capture.go:.*CaptureResumeID`,
		`internal/coding-agents/deepseek/capture.go:.*scanSessions`,
		`internal/lifecycle/markers.go:.*Init`,
		`internal/lifecycle/markers.go:.*markersHook`,
		`internal/lifecycle/verify.go:.*BeginVerifyResume`,
		`internal/lifecycle/markers.go:.*eventToPhaseVerb`,
		`internal/resolver/agent.go:.*Agent`,
		`internal/resolver/agent.go:.*agentFromStoreLazy`,
		`internal/resolver/agent.go:.*lookupAgent`,
		`internal/resolver/agent.go:.*persistAgent`,
		`internal/resolver/agent.go:.*readToolModel`,
		`internal/resolver/mustread.go:.*ParseMustRead`,
		`internal/resolver/source.go:.*FetchLinearBody`,
		`internal/resolver/source.go:.*StartTargetFromLinear`,
		`internal/store/settings.go:.*readSetting`,
		`internal/store/store.go:.*DefaultDir`,
		`internal/store/store.go:.*DefaultPath`,
		`internal/store/store.go:.*DefaultTasksDBPath`,
		`internal/store/store.go:.*DefaultTasksDir`,
		`internal/store/store.go:.*EnsureProject`,
		`internal/store/store.go:.*EnsureTaskDir`,
		`internal/store/store.go:.*ProjectInitialized`,
		`internal/store/store.go:.*ProjectName`,
		`internal/store/store.go:.*RemoveTaskDir`,
		`internal/store/store.go:.*touchBoltFile`,
		`internal/store/tasks/sort.go`,
		`internal/store/tasks/task.go:.*DisplayToolModel`,
		`internal/util/run/spawn.go:.*Spawn`,
		`internal/store/tasks/task.go:.*GetTask`,
		`internal/store/tasks/task.go:.*ListTasks`,
		`internal/util/agentlog/agentlog.go:.*Emit`,
		`internal/util/agentlog/agentlog.go:.*formatValue`,
		`internal/util/run/run.go:.*RunIn`,
		`internal/agents/worker/run.go:.*lookupResumeAgent`,
		`internal/agents/worker/run.go:.*resolveWorker`,
		`internal/lifecycle/tuiquit/tuiquit.go:.*DecidePlan`,
		`internal/store/tasks/task.go:.*DeleteTask`,
	}
	for _, fragment := range mustBeAbsent {
		if strings.Contains(body, fragment) {
			t.Errorf(
				"allowlist still contains %q; "+
					"the symbol must stay covered by tests, not re-allowlisted",
				fragment,
			)
		}
	}
}
