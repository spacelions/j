package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/prompts"
)

// TestContracts_PMQAToneInSaveSuffix pins AC#3: the planner save
// suffix must instruct the agent to write requirements.md as a
// PM/QA-style spec (behavioural acceptance criteria, optional user
// story, optional out-of-scope) and explicitly forbid implementation
// detail (file paths, function signatures, internal architecture,
// implementation steps) inside requirements.md — those belong in
// plan.md. The first-line "concise one-line summary" rule must also
// remain (so `j tasks` summary derivation still works).
func TestContracts_PMQAToneInSaveSuffix(t *testing.T) {
	got := prompts.AppendPlannerSaveSuffix(
		"BASE", "/abs/.j/tasks/T1/requirements.md",
		"/abs/.j/tasks/T1/plan.md",
		"/abs/.j/tasks/T1/clarification.md",
	)
	for _, want := range []string{
		"PM/QA-style spec",
		"acceptance criteria",
		"file paths",
		"function signatures",
		"implementation steps",
		"belong in plan.md",
		"plan.md is the technical companion",
		"one-line summary",
		"# Requirements",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("save suffix missing %q: %q", want, got)
		}
	}
	if !strings.HasPrefix(got, "BASE\n\n") {
		t.Fatalf("suffix should follow base verbatim: %q", got)
	}
}
