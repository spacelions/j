package cursor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacelions/j/internal/testutil"
)

func TestCodeReview_Interactive(t *testing.T) {
	dir := t.TempDir()
	calls := installStub(t, "", 0)
	pid, err := New().CodeReview(t.Context(),
		testutil.CodeReviewRequest(dir, "opus", true))
	require.NoError(t, err)
	assert.Equal(t, 0, pid)
	joined := strings.Join(readCalls(t, calls), " ")
	assert.Contains(t, joined, "--model")
	assert.Contains(t, joined, "code-review planner")
}

func TestCodeReview_Headless(t *testing.T) {
	dir := t.TempDir()
	calls := installStub(t, "", 0)
	pid, err := New().CodeReview(t.Context(),
		testutil.CodeReviewRequest(dir, "opus", false))
	require.NoError(t, err)
	assert.NotEqual(t, 0, pid)
	joined := strings.Join(waitForCalls(t, calls, 5), " ")
	assert.Contains(t, joined, "--print")
}

func TestCodeReview_Interactive_RunnerError(t *testing.T) {
	dir := t.TempDir()
	installStub(t, "", 1)
	_, err := New().CodeReview(t.Context(),
		testutil.CodeReviewRequest(dir, "opus", true))
	require.Error(t, err)
}
