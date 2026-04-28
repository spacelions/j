package codingagents

import (
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
