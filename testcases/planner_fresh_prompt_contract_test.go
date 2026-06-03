package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

func TestPlannerFreshPromptDetailedTechnicalContract(t *testing.T) {
	got := prompts.PlanPrompt(codingagents.PlanRequest{
		FromFilePath:           "/work/request.md",
		RequirementsOutputPath: "/work/.j/tasks/T/requirements.md",
		PlanOutputPath:         "/work/.j/tasks/T/plan.md",
		ClarificationPath:      "/work/.j/tasks/T/clarification.md",
		MustRead:               []string{"AGENTS.md"},
	})

	wants := []string{
		"Before starting, read these project files",
		"- AGENTS.md",
		"Read the user request at \"/work/request.md\"",
		"Do not write code.",
		"implementation-ready technical plan",
		"numbered implementation steps",
		"files and packages likely to touch",
		"methods, functions, prompt fragments, or tests",
		"architecture, lifecycle, or data-flow notes",
		"edge cases and risks",
		"verification commands and acceptance criteria",
		"Save the plan to \"/work/.j/tasks/T/plan.md\"",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("fresh planner prompt missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, "short, concrete plan") {
		t.Fatalf("fresh planner prompt still asks for a short plan:\n%s", got)
	}
}
