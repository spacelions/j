package work

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

// This file holds the work orchestrator's settings-bucket wiring
// tests: lazy-open of `.j/settings`, the resolver.Interactive
// precedence (explicit > stored > cobra default), and the
// fallback-to-prompt path when the settings DB is missing or
// corrupt. The pure resolver.AgentFromStore semantics live in
// internal/cli/picker/agent_test.go and are intentionally
// not re-asserted here.

// Interactive-flag precedence (explicit > stored > default true) is
// pinned in internal/resolver/interactive_test.go. Run consumes the
// resolved bool directly.

// TestRun_FromStore_NilStore_EmptyStorePromptsPick exercises the
// lazy-open success path of resolver.Agent +
// resolver.Interactive + resolver.Agent (persist) on a real
// `.j/settings`: an empty worker bucket triggers
// ErrNoStoredSelection, Run falls back to Pick, and the
// persistence path then re-opens settings, writes, and closes.
func TestRun_FromStore_NilStore_EmptyStorePromptsPick(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "body", "")
	agent := newScriptedAgent()
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
		Store:  nil,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should fall back to prompt: %q", stderr.String())
	}
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	got, ok, err := s.Get(store.BucketWorker, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != "cursor" {
		t.Fatalf("worker.tool = %q (ok=%v), want cursor", got, ok)
	}
}

// TestRun_FromStore_NilStore_SettingsOpenFails covers the lazy
// open-fails branches: with no caller-supplied Store and a
// `<cwd>/.j/settings` directory (instead of file) sabotaging
// bolt.Open, resolver.Agent and resolver.Interactive both
// surface the store.OpenSettings warning and fall back to the
// prompted Pick path.
func TestRun_FromStore_NilStore_SettingsOpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "x", "body", "")
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
	agent := newScriptedAgent()
	var stderr bytes.Buffer
	err = Run(context.Background(), Options{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: &stderr,
		Agents: []codingagents.Agent{agent},
		UI:     &scriptedUI{},
		Store:  nil,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr.String(), "settings") {
		t.Fatalf("stderr should warn about settings open: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should fall back to prompt: %q", stderr.String())
	}
	if agent.worked != 1 {
		t.Fatalf("agent.Work calls = %d, want 1", agent.worked)
	}
}
