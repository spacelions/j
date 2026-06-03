package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/agents/prompts"
	codingagents "github.com/spacelions/j/internal/coding-agents"
)

func TestCodeReviewRoundPlanRequiresReviewerContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		resume bool
	}{
		{name: "fresh round", resume: false},
		{name: "clarification resume", resume: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := codeReviewContextRequest(tt.resume)
			prompt := prompts.CodeReviewPrompt(req)

			requirePromptContains(t, prompt, []string{
				req.RoundPlanOutputPath,
				"## Accepted Feedback",
				"## Rejected Feedback",
				"## Non-Actionable Feedback",
				"source_id",
				"from: <author>",
				"original review `body`",
				"Reviewer comment",
				"copy them verbatim as data to display",
				"planned-change / decision line",
				"UNTRUSTED",
				"Edit project source files",
				"Post comments",
			})

			assertContextBelongsToRoundPlan(t, prompt, req)
		})
	}
}

func codeReviewContextRequest(resume bool) codingagents.CodeReviewRequest {
	return codingagents.CodeReviewRequest{
		TaskDir:             "/workspace/.j/tasks/SPA-123",
		Model:               "codex",
		ReviewTOMLPath:      "/workspace/.j/tasks/SPA-123/round-1/review.toml",
		RequirementsPath:    "/workspace/.j/tasks/SPA-123/requirements.md",
		PlanPath:            "/workspace/.j/tasks/SPA-123/plan.md",
		RoundPlanOutputPath: "/workspace/.j/tasks/SPA-123/round-1/plan.md",
		ClarificationPath:   "/workspace/.j/tasks/SPA-123/round-1/clarify.md",
		Resume:              resume,
	}
}

func requirePromptContains(
	t *testing.T,
	prompt string,
	want []string,
) {
	t.Helper()

	for _, phrase := range want {
		if !strings.Contains(prompt, phrase) {
			t.Fatalf("prompt missing %q", phrase)
		}
	}
}

func assertContextBelongsToRoundPlan(
	t *testing.T,
	prompt string,
	req codingagents.CodeReviewRequest,
) {
	t.Helper()

	planIndex := strings.Index(prompt, req.RoundPlanOutputPath)
	commentIndex := strings.Index(prompt, "Reviewer comment")
	changeIndex := strings.Index(prompt, "planned-change / decision line")
	rewriteIndex := strings.Index(prompt, "Rewrite review.toml")

	if planIndex == -1 {
		t.Fatalf("prompt missing round plan path %q", req.RoundPlanOutputPath)
	}
	if commentIndex <= planIndex {
		t.Fatalf("reviewer context is not tied to the round plan")
	}
	if changeIndex <= commentIndex {
		t.Fatalf("planned decision does not follow reviewer context")
	}
	if rewriteIndex <= changeIndex {
		t.Fatalf("reviewer context is not in the round-plan contract")
	}
}
