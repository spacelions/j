package deepseek

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
	// the interactive deepseek invocation doesn't put the prompt on
	// the command line — it's plumbed via the TUI's stdin. The argv
	// should however include the workspace name.
	assert.Contains(t, strings.Join(readCalls(t, calls), " "), dir)
}

func TestCodeReview_Headless(t *testing.T) {
	dir := t.TempDir()
	calls := installStub(t, "", 0)
	pid, err := New().CodeReview(t.Context(),
		testutil.CodeReviewRequest(dir, "opus", false))
	require.NoError(t, err)
	assert.NotEqual(t, 0, pid)
	assert.Contains(t, strings.Join(waitForCalls(t, calls, 3), " "), "exec")
}
