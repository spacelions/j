package settings

import (
	"bytes"
	"strings"
	"testing"
)

func TestNew_Smoke(t *testing.T) {
	cmd := New()
	if cmd == nil {
		t.Fatal("New returned nil")
	}
	if cmd.Use != "settings" {
		t.Fatalf("Use = %q, want settings", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil; bare `j settings` would fail")
	}

	var found struct{ init_, show bool }
	for _, sub := range cmd.Commands() {
		switch sub.Use {
		case "init":
			found.init_ = true
		case "show":
			found.show = true
		}
	}
	if !found.init_ || !found.show {
		t.Fatalf("missing subcommands: %+v", found)
	}
}

// TestNew_BareSettingsRunsShow exercises the parent RunE so plain
// `j settings` is equivalent to `j settings show`. We chdir into a
// temp dir without an existing DB and assert the missing-DB hint.
func TestNew_BareSettingsRunsShow(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout.String(), "j settings init") {
		t.Fatalf("stdout = %q, want hint to run init", stdout.String())
	}
}
