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
// tests: lazy-open of `.j/settings`, the resolveInteractive
// precedence (explicit > stored > cobra default), and the
// fallback-to-prompt path when the settings DB is missing or
// corrupt. The pure agentpick.FromStore semantics live in
// internal/cli/agentpick/agentpick_test.go and are intentionally
// not re-asserted here.

// TestRun_FromStore_InteractivePrecedence collapses the four
// resolveInteractive precedence scenarios into a single table:
// stored-false-overrides-default, explicit-wins-over-stored,
// stored-unparseable-falls-to-default, and no-interactive-key.
func TestRun_FromStore_InteractivePrecedence(t *testing.T) {
	cases := []struct {
		name               string
		bucket             map[string]string
		explicit           *bool
		wantInteractive    bool
		wantNoStderrWarn   bool
		wantPersistedValue string
	}{
		{
			name:               "StoredFalseOverridesDefault",
			bucket:             map[string]string{"tool": "cursor", "model": "sonnet-4", "interactive": "false"},
			explicit:           nil,
			wantInteractive:    false,
			wantPersistedValue: "false",
		},
		{
			name:            "ExplicitWinsOverStored",
			bucket:          map[string]string{"tool": "cursor", "model": "sonnet-4", "interactive": "false"},
			explicit:        boolPtr(true),
			wantInteractive: true,
		},
		{
			name:             "StoredUnparseableFallsToDefault",
			bucket:           map[string]string{"tool": "cursor", "model": "sonnet-4", "interactive": "garbage"},
			explicit:         nil,
			wantInteractive:  true,
			wantNoStderrWarn: true,
		},
		{
			name:            "NoInteractiveKeyKeepsDefault",
			bucket:          map[string]string{"tool": "cursor", "model": "sonnet-4"},
			explicit:        nil,
			wantInteractive: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			for k, v := range tc.bucket {
				if err := s.Put(store.BucketWorker, k, v); err != nil {
					t.Fatalf("Put(%s,%s): %v", k, v, err)
				}
			}
			id := seedPlanDoneTask(t, "x", "body", "")
			agent := newScriptedAgent()
			var stderr bytes.Buffer
			err := Run(context.Background(), Options{
				TaskID:      id,
				Stdin:       strings.NewReader(""),
				Stdout:      io.Discard,
				Stderr:      &stderr,
				Agents:      []codingagents.Agent{agent},
				UI:          &scriptedUI{},
				Store:       s,
				Interactive: tc.explicit,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if agent.lastReq.Interactive != tc.wantInteractive {
				t.Fatalf("Interactive = %v, want %v", agent.lastReq.Interactive, tc.wantInteractive)
			}
			if tc.wantNoStderrWarn && strings.Contains(stderr.String(), "interactive") {
				t.Fatalf("stderr should not warn on unparseable interactive: %q", stderr.String())
			}
			if tc.wantPersistedValue != "" {
				if v, ok := mustGet(t, s, "interactive"); !ok || v != tc.wantPersistedValue {
					t.Fatalf("worker.interactive = %q (ok=%v), want %q", v, ok, tc.wantPersistedValue)
				}
			}
		})
	}
}

// TestRun_FromStore_NilStore_EmptyStorePromptsPick exercises the
// lazy-open success path of workerFromStore +
// storedWorkerInteractive + persistWorkerSelection on a real
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
// bolt.Open, workerFromStore and storedWorkerInteractive both
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
	if !strings.Contains(stderr.String(), "warning: settings") {
		t.Fatalf("stderr should warn about settings open: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Choose your favourite:") {
		t.Fatalf("stderr should fall back to prompt: %q", stderr.String())
	}
	if agent.worked != 1 {
		t.Fatalf("agent.Work calls = %d, want 1", agent.worked)
	}
}
