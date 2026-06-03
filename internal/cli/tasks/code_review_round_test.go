package tasks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

func setupTask(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	testutil.Init(t)
	const id = "01-task"
	_, err := tasks.EnsureDir(id)
	require.NoError(t, err)
	return id
}

func TestResolveOrAllocateRound_FirstRound(t *testing.T) {
	id := setupTask(t)
	round, err := resolveOrAllocateRound(id)
	require.NoError(t, err)
	assert.Equal(t, 1, round.N)
	assert.DirExists(t, round.Dir)
	assert.True(t, filepath.IsAbs(round.ReviewTOMLPath))
}

func TestResolveOrAllocateRound_NextRound(t *testing.T) {
	id := setupTask(t)
	first, err := resolveOrAllocateRound(id)
	require.NoError(t, err)
	require.Equal(t, 1, first.N)
	second, err := resolveOrAllocateRound(id)
	require.NoError(t, err)
	// Without a clarification.md, the allocator advances to round-2.
	assert.Equal(t, 2, second.N)
}

func TestResolveOrAllocateRound_ResumeOnClarification(t *testing.T) {
	id := setupTask(t)
	first, err := resolveOrAllocateRound(id)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		first.ClarificationPath, []byte("?"), 0o644))
	again, err := resolveOrAllocateRound(id)
	require.NoError(t, err)
	assert.Equal(t, first.N, again.N,
		"clarification.md must keep the round number stable")
}

func TestResolveOrAllocateRound_IgnoresJunk(t *testing.T) {
	id := setupTask(t)
	round, err := resolveOrAllocateRound(id)
	require.NoError(t, err)
	junk := filepath.Join(filepath.Dir(round.Dir), "round-zero")
	require.NoError(t, os.MkdirAll(junk, 0o755))
	stray := filepath.Join(filepath.Dir(round.Dir), "summary.md")
	require.NoError(t, os.WriteFile(stray, []byte(""), 0o644))
	next, err := resolveOrAllocateRound(id)
	require.NoError(t, err)
	assert.Equal(t, 2, next.N)
}

func TestParseRoundDir(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"round-1", 1, true},
		{"round-42", 42, true},
		{"round-", 0, false},
		{"round-0", 0, false},
		{"round-x", 0, false},
		{"summary.md", 0, false},
	}
	for _, c := range cases {
		got, ok := parseRoundDir(c.in)
		assert.Equal(t, c.ok, ok, "input %q", c.in)
		if ok {
			assert.Equal(t, c.want, got)
		}
	}
}

func TestParseRoundFlag(t *testing.T) {
	assert.Equal(t, 0, parseRoundFlag(""))
	assert.Equal(t, 0, parseRoundFlag("nope"))
	assert.Equal(t, 0, parseRoundFlag("-2"))
	assert.Equal(t, 3, parseRoundFlag("3"))
}

// TestCodeReviewArtifactsKeepCanonical pins the canonical-untouched
// invariant: allocating new rounds must not touch the task's
// requirements.md / plan.md / task.toml files.
func TestCodeReviewArtifactsKeepCanonical(t *testing.T) {
	id := setupTask(t)
	taskDir := filepath.Join(tasks.DefaultDir(), id)
	require.NoError(t, os.WriteFile(
		filepath.Join(taskDir, tasks.RequirementsFileName),
		[]byte("orig req"), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(taskDir, tasks.PlanFileName),
		[]byte("orig plan"), 0o644))
	_, err := resolveOrAllocateRound(id)
	require.NoError(t, err)
	req, _ := os.ReadFile(filepath.Join(taskDir, tasks.RequirementsFileName))
	plan, _ := os.ReadFile(filepath.Join(taskDir, tasks.PlanFileName))
	assert.Equal(t, "orig req", string(req))
	assert.Equal(t, "orig plan", string(plan))
}
