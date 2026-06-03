package claude

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacelions/j/internal/testutil"
)

func TestCodeReview_Interactive(t *testing.T) {
	dir := t.TempDir()
	calls, _ := installStub(t, "", 0)
	pid, err := New().CodeReview(t.Context(),
		testutil.CodeReviewRequest(dir, "opus", true))
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
		testutil.CodeReviewRequest(dir, "opus", false))
	require.NoError(t, err)
	assert.NotEqual(t, 0, pid, "headless must return a non-zero PID")
	joined := strings.Join(waitForCalls(t, calls, 5), " ")
	assert.Contains(t, joined, "--print")
}

func TestCodeReview_Headless_SpawnError(t *testing.T) {
	dir := t.TempDir()
	req := testutil.CodeReviewRequest(dir, "opus", false)
	req.AgentLogPath = "" // trip the spawn error
	_, err := New().CodeReview(t.Context(), req)
	require.Error(t, err)
}
