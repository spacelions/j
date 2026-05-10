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
		`internal/store/tasks/sort.go`,
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
