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
		`internal/cli/tasks/cmd.go:.*writeTasks`,
		`internal/cli/tasks/orchestrate_cmd.go:.*newOrchestrateCmd`,
		`internal/cli/tasks/resume_plan.go:.*RunResumePlan`,
		`internal/coding-agents/cursor/cursor.go:.*CreateChatID`,
		`internal/lifecycle/verify.go:.*BeginVerifyResume`,
		`internal/resolver/agent.go:.*Agent`,
		`internal/resolver/agent.go:.*lookupAgent`,
		`internal/resolver/agent.go:.*readToolModel`,
		`internal/resolver/mustread.go:.*ParseMustRead`,
		`internal/resolver/source.go:.*StartTargetFromLinear`,
		`internal/store/tasks/sort.go`,
		`internal/store/tasks/task.go:.*DisplayToolModel`,
		`internal/util/run/spawn.go:.*Spawn`,
		`internal/store/tasks/task.go:.*GetTask`,
		`internal/store/tasks/task.go:.*ListTasks`,
		`internal/util/agentlog/agentlog.go:.*Emit`,
		`internal/util/agentlog/agentlog.go:.*formatValue`,
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
