package codingagents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultWorkspace(t *testing.T) {
	got := DefaultWorkspace(filepath.FromSlash("/tmp/foo/spec.md"))
	want := filepath.FromSlash("/tmp/foo")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestProjectRootWorkspace pins the happy-path contract: the helper
// returns the canonicalised cwd as an absolute path. t.Chdir into a
// temp dir and compare against os.Getwd so the assertion survives
// macOS's /var -> /private/var symlink hop.
func TestProjectRootWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	got := ProjectRootWorkspace()
	if got != cwd {
		t.Fatalf("ProjectRootWorkspace = %q, want %q", got, cwd)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("ProjectRootWorkspace returned non-absolute %q", got)
	}
}
