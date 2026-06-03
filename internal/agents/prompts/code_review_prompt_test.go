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

// TestCodeReviewPrompt_DocumentsTOMLLifecycle pins P11: the prompt
// must say that review.toml is pre-populated and that the planner
// only appends decisions.
func TestCodeReviewPrompt_DocumentsTOMLLifecycle(t *testing.T) {
	got := CodeReviewPrompt(sampleCodeReviewRequest())
	for _, phrase := range []string{
		"pre-populated",
		"APPEND",
		"do not add or remove rows",
	} {
		assert.Contains(t, got, phrase,
			"prompt missing TOML-lifecycle wording %q", phrase)
	}
}

// TestCodeReviewPrompt_PrependsMustRead pins P10.
func TestCodeReviewPrompt_PrependsMustRead(t *testing.T) {
	req := sampleCodeReviewRequest()
	req.MustRead = []string{"AGENTS.md", "docs/style.md"}
	got := CodeReviewPrompt(req)
	for _, path := range req.MustRead {
		assert.Contains(t, got, path,
			"prompt must include must-read entry %q", path)
	}
	headerIdx := strings.Index(got, "AGENTS.md")
	bodyIdx := strings.Index(got, "You are the code-review planner")
	assert.Less(t, headerIdx, bodyIdx,
		"must-read header must come before the role body")
}

// TestCodeReviewPrompt_ResumeUsesClarificationTemplate pins P9.
func TestCodeReviewPrompt_ResumeUsesClarificationTemplate(t *testing.T) {
	req := sampleCodeReviewRequest()
	req.Resume = true
	got := CodeReviewPrompt(req)
	assert.Contains(t, got,
		"You are the code-review planner resuming",
		"resume mode must use the clarification-resume template")
	assert.Contains(t, got, "delete",
		"resume prompt must tell the planner to delete clarification.md")
	// And the regular template is NOT used: the fresh template's
	// "produce a code-review round plan" sentence should not appear.
	assert.NotContains(t, got,
		"and produce a code-review round plan")
}

// TestCodeReviewPrompt_FreshNotResume pins that the resume body
// does NOT leak into the fresh-run template when Resume=false.
func TestCodeReviewPrompt_FreshNotResume(t *testing.T) {
	got := CodeReviewPrompt(sampleCodeReviewRequest())
	assert.NotContains(t, got, "resuming a previous round")
}
