package plan

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// This file holds the plan orchestrator's settings-bucket wiring
// tests: lazy-open of `.j/settings`, the resolver.Interactive
// precedence (explicit > stored > cobra default), and the
// fallback-to-prompt path when the settings DB is missing or
// corrupt. The pure resolver.AgentFromStore semantics live in
// internal/cli/picker/agent_test.go and are intentionally
// not re-asserted here.

// Interactive-flag precedence (explicit > stored > default true) is
// pinned in internal/resolver/interactive_test.go. Run consumes the
// resolved bool directly.

// TestRun_FromStore_EmptyStore_PromptsPick covers the lazy-open
// success branch of resolver.Agent + resolver.Agent (persist):
// an empty project store on the markdown-import path triggers
// "Choose your favourite:", runs Pick, and writes the chosen
// agent back to `<cwd>/.j/settings`.
func TestRun_FromStore_EmptyStore_PromptsPick(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
		Store:    nil,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr = %q, want choose-your-favourite line", stderr.String())
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not created: %v", err)
	}
}

// TestRun_FromStore_NilStore_SettingsOpenFails covers the lazy
// open-fails branches: with no caller-supplied Store and a
// `<cwd>/.j/settings` directory (instead of file) sabotaging
// bolt.Open, resolver.Agent and resolver.Interactive both
// surface the store.OpenSettings warning and fall back to the
// prompted Pick path while still running the agent.
func TestRun_FromStore_NilStore_SettingsOpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	settingsPath, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settingsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	var stderr bytes.Buffer
	err = Run(context.Background(), Options{
		FromFile: target,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
		Store:    nil,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: settings") {
		t.Fatalf("stderr should warn about settings open: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should fall back to prompt: %q", stderr.String())
	}
	if agent.planned != 1 {
		t.Fatalf("agent.Plan calls = %d, want 1", agent.planned)
	}
}
