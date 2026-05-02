package web

import (
	"strings"
	"testing"
)

func TestNew_Smoke(t *testing.T) {
	cmd := New()
	if cmd == nil {
		t.Fatal("New returned nil")
	}
	if cmd.Use != "web" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "web")
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil")
	}
}

// TestNew_RunE_FailsWithoutSettings exercises the missing-store path:
// without a `.j/settings` file the loader surfaces a wrapped
// fs.ErrNotExist whose message points the user at `j init`.
func TestNew_RunE_FailsWithoutSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	err := New().RunE(nil, nil)
	if err == nil {
		t.Fatal("expected error when settings store is missing")
	}
	if !strings.Contains(err.Error(), "j init") {
		t.Fatalf("err = %v, want hint to run `j init`", err)
	}
}
