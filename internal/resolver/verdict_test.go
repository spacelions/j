package resolver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVerdict(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body string
		want string
	}{
		{"pass", "notes\n  VERDICT: pass\n", "PASS"},
		{"fail", "VERDICT: FAIL\r\n", "FAIL"},
		{"malformed", "VERDICT: MAYBE\n", "FAIL"},
		{"empty", "\n\n", "FAIL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".md")
			require.NoError(t, os.WriteFile(path, []byte(tc.body), 0o644))
			assert.Equal(t, tc.want, ParseVerdict(path))
		})
	}
	assert.Equal(t, "FAIL", ParseVerdict(filepath.Join(dir, "missing.md")))
}

func TestReadVerdictForTask(t *testing.T) {
	setupResolverProject(t)
	dir, err := tasks.EnsureDir("task")
	require.NoError(t, err)
	path := filepath.Join(dir, tasks.VerifierFindingsFileName)
	require.NoError(t, os.WriteFile(path, []byte("VERDICT: PASS\n"), 0o644))
	assert.Equal(t, "PASS", ReadVerdictForTask("task"))
}
