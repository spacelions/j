package codingagents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultWorkspace(t *testing.T) {
	got := DefaultWorkspace(filepath.FromSlash("/tmp/foo/spec.md"))
	want := filepath.FromSlash("/tmp/foo")
	assert.Equal(t, want, got)
}

// TestProjectRootWorkspace pins the happy-path contract: the helper
// returns the canonicalised cwd as an absolute path. t.Chdir into a
// temp dir and compare against os.Getwd so the assertion survives
// macOS's /var -> /private/var symlink hop.
func TestProjectRootWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cwd, err := os.Getwd()
	require.NoError(t, err, "Getwd")
	got := ProjectRootWorkspace()
	assert.Equal(t, cwd, got)
	assert.Truef(t, filepath.IsAbs(got), "%q is not absolute", got)
}
