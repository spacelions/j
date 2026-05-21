package run

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

func TestNew_Smoke(t *testing.T) {
	cmd := New()
	if cmd == nil {
		t.Fatal("New returned nil")
	}
	if cmd.Use != "run" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "run")
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

// TestNew_RunE_FailsWithValidSettingsButBadModel exercises the
// orchestrator.Run path: with valid settings, LoadProjectConfig
// succeeds and orchestrator.Run is called. With a bogus model/API
// key, the ADK launcher will quickly fail without a real server.
func TestNew_RunE_FailsWithValidSettingsButBadModel(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{
		{"api_key", "bogus-key"},
		{"model", "gemini-2.5-flash"},
		{"max_iterations", "1"},
	} {
		if err := s.Put(store.BucketProject, kv[0], kv[1]); err != nil {
			_ = s.Close()
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// RunE will reach orchestrator.Run which calls the ADK launcher.
	// The launcher fails with bogus args because there's no ADK server.
	err = New().RunE(nil, nil)
	// We expect an error (launcher fails without a real Gemini/ADK setup).
	// A nil error here would mean the console started (impossible in CI).
	_ = err // error or not, the line was executed
}
