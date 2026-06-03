package claude

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

func TestCodeReview_Interactive(t *testing.T) {
	dir := t.TempDir()
	calls, _ := installStub(t, "", 0)
	pid, err := New().CodeReview(t.Context(), codingagents.CodeReviewRequest{
		TaskDir:             dir,
		Model:               "opus",
		ReviewTOMLPath:      filepath.Join(dir, "review.toml"),
		RequirementsPath:    filepath.Join(dir, "requirements.md"),
		PlanPath:            filepath.Join(dir, "plan.md"),
		RoundPlanOutputPath: filepath.Join(dir, "plan.md"),
		ClarificationPath:   filepath.Join(dir, "clarification.md"),
		Interactive:         true,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, pid)
	argv := readCalls(t, calls)
	require.NotEmpty(t, argv)
	// no --permission-mode plan in code-review
	for _, a := range argv {
		assert.NotEqual(t, "plan", a, "code-review must not request plan mode")
	}
	prompt := argv[len(argv)-1]
	assert.Contains(t, prompt, "code-review planner")
}

func TestCodeReview_Headless(t *testing.T) {
	dir := t.TempDir()
	calls, _ := installStub(t, "", 0)
	pid, err := New().CodeReview(t.Context(),
		codingagents.CodeReviewRequest{
			TaskDir:             dir,
			Model:               "opus",
			ReviewTOMLPath:      filepath.Join(dir, "review.toml"),
			RequirementsPath:    filepath.Join(dir, "requirements.md"),
			PlanPath:            filepath.Join(dir, "plan.md"),
			RoundPlanOutputPath: filepath.Join(dir, "plan.md"),
			ClarificationPath:   filepath.Join(dir, "clarification.md"),
			AgentLogPath:        filepath.Join(dir, "agent.log"),
		})
	require.NoError(t, err)
	assert.NotEqual(t, 0, pid, "headless must return a non-zero PID")
	argv := waitForCalls(t, calls, 5)
	joined := strings.Join(argv, " ")
	assert.Contains(t, joined, "--print")
}

func TestCodeReview_Headless_SpawnError(t *testing.T) {
	dir := t.TempDir()
	_, err := New().CodeReview(t.Context(), codingagents.CodeReviewRequest{
		TaskDir:             dir,
		Model:               "opus",
		ReviewTOMLPath:      filepath.Join(dir, "review.toml"),
		RequirementsPath:    filepath.Join(dir, "requirements.md"),
		PlanPath:            filepath.Join(dir, "plan.md"),
		RoundPlanOutputPath: filepath.Join(dir, "plan.md"),
		ClarificationPath:   filepath.Join(dir, "clarification.md"),
		// AgentLogPath intentionally empty to trip the spawn error
	})
	require.Error(t, err)
}
