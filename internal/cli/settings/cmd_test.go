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

	var found struct{ set, reset bool }
	for _, sub := range cmd.Commands() {
		switch {
		case strings.HasPrefix(sub.Use, "set "):
			found.set = true
		case strings.HasPrefix(sub.Use, "reset "):
			found.reset = true
		}
	}
	if !found.set || !found.reset {
		t.Fatalf("missing subcommands: %+v", found)
	}
}

// TestNew_BareSettingsRunsList exercises the parent RunE: plain
// `j settings` is the list path. After mustInit the DB carries only
// the project.mustread placeholder seeded by preflight, but the four
// known section headers always render in fixed order.
func TestNew_BareSettingsRunsList(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "[project]\n" +
		"  mustread = \n" +
		"\n" +
		"[planner]\n" +
		"\n" +
		"[worker]\n" +
		"\n" +
		"[verifier]\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}
