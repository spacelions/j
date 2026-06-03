package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

func TestPlannerClarificationResumeKeepsSaveAndQuestionContracts(t *testing.T) {
	got := prompts.PlanPrompt(codingagents.PlanRequest{
		FromFilePath:            "/work/request.md",
		RequirementsOutputPath:  "/work/.j/tasks/T/requirements.md",
		PlanOutputPath:          "/work/.j/tasks/T/plan.md",
		ClarificationPath:       "/work/.j/tasks/T/clarification.md",
		Resume:                  true,
		ResumeFromClarification: true,
	})

	wants := []string{
		"paused with an open question",
		"Read that file",
		"restate the question to the user",
		"delete \"/work/.j/tasks/T/clarification.md\"",
		"Save the (possibly refined) requirements summary",
		"Save the plan to \"/work/.j/tasks/T/plan.md\"",
		"numbered implementation steps",
		"acceptance criteria",
		"If you need clarification before you can finish",
		"write your question to \"/work/.j/tasks/T/clarification.md\"",
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("clarification resume prompt missing %q:\n%s", want, got)
		}
	}
}
