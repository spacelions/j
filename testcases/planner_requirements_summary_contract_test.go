package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

func TestPlannerRequirementsSummaryStaysProductQAOriented(t *testing.T) {
	got := prompts.PlanPrompt(codingagents.PlanRequest{
		FromFilePath:           "/work/request.md",
		RequirementsOutputPath: "/work/.j/tasks/T/requirements.md",
		PlanOutputPath:         "/work/.j/tasks/T/plan.md",
		ClarificationPath:      "/work/.j/tasks/T/clarification.md",
	})

	wants := []string{
		"Save the (possibly refined) requirements summary",
		"PM/QA-style spec",
		"include user",
		"story, behavioral acceptance criteria",
		"Do not include file paths",
		"file paths, function signatures, internal architecture",
		"implementation steps; those belong in plan.md",
		"plan.md is the technical companion to requirements.md",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("planner save contract missing %q:\n%s", want, got)
		}
	}

	reqIndex := strings.Index(got, "Save the (possibly refined)")
	planIndex := strings.Index(got, "Save the plan to")
	if reqIndex < 0 || planIndex < 0 || reqIndex > planIndex {
		t.Fatalf("requirements guidance should precede plan guidance:\n%s", got)
	}
}
