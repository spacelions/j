package deepseek

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	codingagents "github.com/spacelions/j/internal/coding-agents"
)

func crReq(dir string, interactive bool) codingagents.CodeReviewRequest {
	return codingagents.CodeReviewRequest{
		TaskDir:             dir,
		Model:               "opus",
		ReviewTOMLPath:      filepath.Join(dir, "review.toml"),
		RequirementsPath:    filepath.Join(dir, "requirements.md"),
		PlanPath:            filepath.Join(dir, "plan.md"),
		RoundPlanOutputPath: filepath.Join(dir, "plan.md"),
		ClarificationPath:   filepath.Join(dir, "clarification.md"),
		Interactive:         interactive,
		AgentLogPath:        filepath.Join(dir, "agent.log"),
	}
}

func TestCodeReview_Interactive(t *testing.T) {
	dir := t.TempDir()
	calls := installStub(t, "", 0)
	pid, err := New().CodeReview(t.Context(), crReq(dir, true))
	require.NoError(t, err)
	assert.Equal(t, 0, pid)
	argv := readCalls(t, calls)
	// the interactive deepseek invocation doesn't put the prompt on
	// the command line — it's plumbed via the TUI's stdin. The argv
	// should however include the workspace name.
	assert.Contains(t, strings.Join(argv, " "), dir)
}

func TestCodeReview_Headless(t *testing.T) {
	dir := t.TempDir()
	calls := installStub(t, "", 0)
	pid, err := New().CodeReview(t.Context(), crReq(dir, false))
	require.NoError(t, err)
	assert.NotEqual(t, 0, pid)
	argv := waitForCalls(t, calls, 3)
	assert.Contains(t, strings.Join(argv, " "), "exec")
}
