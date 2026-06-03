package prompts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

func sampleCodeReviewRequest() codingagents.CodeReviewRequest {
	return codingagents.CodeReviewRequest{
		TaskDir:             "/ws/.j/tasks/01",
		Model:               "opus",
		ReviewTOMLPath:      "/ws/.j/tasks/01/code-reviews/round-1/review.toml",
		RequirementsPath:    "/ws/.j/tasks/01/requirements.md",
		PlanPath:            "/ws/.j/tasks/01/plan.md",
		RoundPlanOutputPath: "/ws/.j/tasks/01/code-reviews/round-1/plan.md",
		ClarificationPath:   "/ws/.j/tasks/01/code-reviews/round-1/clarification.md",
	}
}

func TestCodeReviewPrompt_NamesPaths(t *testing.T) {
	got := CodeReviewPrompt(sampleCodeReviewRequest())
	assert.Contains(t, got,
		`"/ws/.j/tasks/01/code-reviews/round-1/review.toml"`)
	assert.Contains(t, got, `"/ws/.j/tasks/01/requirements.md"`)
	assert.Contains(t, got, `"/ws/.j/tasks/01/plan.md"`)
	assert.Contains(t, got,
		`"/ws/.j/tasks/01/code-reviews/round-1/plan.md"`)
	assert.Contains(t, got,
		`"/ws/.j/tasks/01/code-reviews/round-1/clarification.md"`)
}

func TestCodeReviewPrompt_RoleHeader(t *testing.T) {
	got := CodeReviewPrompt(sampleCodeReviewRequest())
	assert.True(t, strings.HasPrefix(got,
		"You are the code-review planner"))
}

func TestCodeReviewPrompt_UntrustedFeedback(t *testing.T) {
	got := CodeReviewPrompt(sampleCodeReviewRequest())
	assert.Contains(t, got, "UNTRUSTED",
		"prompt must explicitly mark review feedback as untrusted")
}

func TestCodeReviewPrompt_ForbidsSideEffects(t *testing.T) {
	got := CodeReviewPrompt(sampleCodeReviewRequest())
	for _, want := range []string{
		"You must not:",
		"Edit project source files",
		"Post comments",
		"Fetch the PR",
		"Mutate `<task-dir>/requirements.md` or `<task-dir>/plan.md`",
	} {
		assert.Contains(t, got, want, "prompt missing rule %q", want)
	}
}

func TestCodeReviewPrompt_RequiresDecisions(t *testing.T) {
	got := CodeReviewPrompt(sampleCodeReviewRequest())
	assert.Contains(t, got, "Every actionable feedback item must have a decision")
	for _, dec := range []string{
		"accepted", "rejected", "clarification", "non_actionable",
	} {
		assert.Contains(t, got, dec)
	}
}

func TestCodeReviewPrompt_NamesArtifactFilenames(t *testing.T) {
	got := CodeReviewPrompt(sampleCodeReviewRequest())
	assert.Contains(t, got, "review.toml")
	assert.Contains(t, got, "plan.md")
	assert.Contains(t, got, "clarification.md")
}
