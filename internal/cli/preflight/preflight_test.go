package preflight

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
)

// scriptedUI is a deterministic UI fake that records prompt invocations
// and answers ConfirmInit / AskMustRead with pre-set values. The error
// fields let tests drive the "prompt errored" branches.
type scriptedUI struct {
	confirm bool
	err     error
	calls   int

	mustReadValue string
	mustReadErr   error
	mustReadCalls int
}

func (u *scriptedUI) ConfirmInit(context.Context) (bool, error) {
	u.calls++
	if u.err != nil {
		return false, u.err
	}
	return u.confirm, nil
}

func (u *scriptedUI) AskMustRead(context.Context) (string, error) {
	u.mustReadCalls++
	if u.mustReadErr != nil {
		return "", u.mustReadErr
	}
	return u.mustReadValue, nil
}

// putMustRead stores a project.must_read value into the freshly-init'd
// settings store at the current cwd so a subsequent Ensure call hits
// the "already set" short-circuit. Used by tests that exercise the
// initialized-and-already-asked path.
func putMustRead(t *testing.T, value string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Put(store.BucketProject, resolver.KeyMustRead, value); err != nil {
		_ = s.Close()
		t.Fatalf("Put: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// readMustRead returns the stored project.must_read value plus a
// "set" flag from the current cwd's settings store.
func readMustRead(t *testing.T) (string, bool) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	v, set, err := s.Get(store.BucketProject, resolver.KeyMustRead)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return v, set
}

// TestEnsure_AlreadyInitialized pins the happy path: with all four
// artifacts present and project.must_read already set, Ensure returns
// nil without ever invoking the UI.
func TestEnsure_AlreadyInitialized(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	putMustRead(t, "AGENTS.md;CLAUDE.md")
	ui := &scriptedUI{}
	var stderr bytes.Buffer
	if err := Ensure(context.Background(), ui, &stderr); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if ui.calls != 0 {
		t.Fatalf("UI should not be prompted on initialized project: %d", ui.calls)
	}
	if ui.mustReadCalls != 0 {
		t.Fatalf("AskMustRead should not fire when must_read set: %d", ui.mustReadCalls)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr should stay empty: %q", stderr.String())
	}
}

// TestEnsure_MissingArtifacts walks every individual missing-artifact
// case so the prompt fires for each. The matrix matches the four
// artifacts owned by EnsureProject.
func TestEnsure_MissingArtifacts(t *testing.T) {
	cases := []struct {
		name   string
		remove string
	}{
		{"jdir", ""},
		{"settings", "settings"},
		{"tasksDir", store.TasksDirName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			if err := store.EnsureProject(); err != nil {
				t.Fatalf("EnsureProject: %v", err)
			}
			if c.remove == "" {
				if err := os.RemoveAll(filepath.Join(dir, ".j")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.RemoveAll(filepath.Join(dir, ".j", c.remove)); err != nil {
					t.Fatal(err)
				}
			}
			ui := &scriptedUI{confirm: true}
			var stderr bytes.Buffer
			err := Ensure(context.Background(), ui, &stderr)
			if !errors.Is(err, ErrNeedsRetry) {
				t.Fatalf("err = %v, want ErrNeedsRetry", err)
			}
			if ui.calls != 1 {
				t.Fatalf("UI calls = %d, want 1", ui.calls)
			}
			if !strings.Contains(stderr.String(), "initialized; please re-run") {
				t.Fatalf("stderr = %q, want re-run breadcrumb", stderr.String())
			}
			ok, err := store.ProjectInitialized()
			if err != nil {
				t.Fatalf("ProjectInitialized: %v", err)
			}
			if !ok {
				t.Fatal("expected project to be initialized after accept-path")
			}
		})
	}
}

// TestEnsure_DeclinePathReturnsNotInitialized pins the user-decline
// branch: Ensure surfaces ErrNotInitialized and does NOT create the
// layout.
func TestEnsure_DeclinePathReturnsNotInitialized(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	ui := &scriptedUI{confirm: false}
	var stderr bytes.Buffer
	err := Ensure(context.Background(), ui, &stderr)
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("err = %v, want ErrNotInitialized", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".j")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf(".j should not exist after decline: stat=%v", statErr)
	}
}

// TestEnsure_UIError surfaces a UI error verbatim.
func TestEnsure_UIError(t *testing.T) {
	t.Chdir(t.TempDir())
	boom := errors.New("ui boom")
	err := Ensure(context.Background(), &scriptedUI{err: boom}, &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

// TestEnsure_EnsureProjectFails forces the EnsureProject error branch
// by pre-creating an unreadable .j path before the user accepts.
func TestEnsure_EnsureProjectFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, ".j"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ui := &scriptedUI{confirm: true}
	err := Ensure(context.Background(), ui, &bytes.Buffer{})
	if err == nil || errors.Is(err, ErrNeedsRetry) {
		t.Fatalf("err = %v, want EnsureProject failure", err)
	}
}

// TestEnsure_PropagatesProjectInitializedError fires when stat on
// .j returns a non-NotExist error.
func TestEnsure_PropagatesProjectInitializedError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file-mode semantics required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode permissions")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	jdir := filepath.Join(dir, ".j")
	if err := os.Chmod(jdir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(jdir, 0o755) })
	if err := Ensure(context.Background(), &scriptedUI{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected stat error to propagate")
	}
}

// TestPreRunE_Initialized covers the cobra wiring on the happy path:
// an already-initialized cwd with project.must_read set makes the helper
// return nil. The huh UI is constructed but never reached because
// Ensure short-circuits.
func TestPreRunE_Initialized(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putMustRead(t, "AGENTS.md")
	cmd := &cobra.Command{}
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(context.Background())
	if err := PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
}

// TestPreRunE_NoContextDefaults exercises the ctx==nil guard so the
// helper falls back to context.Background.
func TestPreRunE_NoContextDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
	putMustRead(t, "AGENTS.md")
	cmd := &cobra.Command{}
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
}

// TestEnsure_PromptsForMustReadWhenMissing pins the new branch:
// project initialized but project.must_read unset -> AskMustRead fires
// once, value persisted verbatim, Ensure returns nil so the user's
// original command proceeds without a re-run.
func TestEnsure_PromptsForMustReadWhenMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	ui := &scriptedUI{mustReadValue: "AGENTS.md;CLAUDE.md"}
	var stderr bytes.Buffer
	if err := Ensure(context.Background(), ui, &stderr); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if ui.mustReadCalls != 1 {
		t.Fatalf("mustReadCalls = %d, want 1", ui.mustReadCalls)
	}
	if ui.calls != 0 {
		t.Fatalf("ConfirmInit should not fire when initialized: %d", ui.calls)
	}
	got, set := readMustRead(t)
	if !set {
		t.Fatal("project.must_read should be persisted")
	}
	if got != "AGENTS.md;CLAUDE.md" {
		t.Fatalf("project.must_read = %q, want preserved-case value", got)
	}
}

// TestEnsure_BlankMustReadIsPersisted: an explicit empty answer is
// stored verbatim and a second Ensure call does NOT re-prompt.
func TestEnsure_BlankMustReadIsPersisted(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	ui := &scriptedUI{mustReadValue: ""}
	if err := Ensure(context.Background(), ui, &bytes.Buffer{}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if ui.mustReadCalls != 1 {
		t.Fatalf("mustReadCalls = %d, want 1", ui.mustReadCalls)
	}
	got, set := readMustRead(t)
	if !set || got != "" {
		t.Fatalf("readMustRead = (%q, %v), want (\"\", true)", got, set)
	}
	if err := Ensure(context.Background(), ui, &bytes.Buffer{}); err != nil {
		t.Fatalf("Ensure (second): %v", err)
	}
	if ui.mustReadCalls != 1 {
		t.Fatalf("mustReadCalls after re-run = %d, want 1 (no re-prompt)", ui.mustReadCalls)
	}
}

// TestEnsure_DoesNotPromptWhenMustReadSet covers the short-circuit
// when project.must_read is already populated.
func TestEnsure_DoesNotPromptWhenMustReadSet(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	putMustRead(t, "AGENTS.md")
	ui := &scriptedUI{}
	if err := Ensure(context.Background(), ui, &bytes.Buffer{}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if ui.mustReadCalls != 0 {
		t.Fatalf("mustReadCalls = %d, want 0", ui.mustReadCalls)
	}
}

// TestEnsure_MustReadUIError surfaces a UI error from AskMustRead
// verbatim so the caller sees the original failure.
func TestEnsure_MustReadUIError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	boom := errors.New("mustRead boom")
	ui := &scriptedUI{mustReadErr: boom}
	err := Ensure(context.Background(), ui, &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

// TestNewHuhUI_NotNil pins the constructor: it returns a non-nil UI
// implementation. Driving the form itself requires a TTY so the
// behaviour beyond construction is exercised through the UI
// interface in headless tests.
func TestNewHuhUI_NotNil(t *testing.T) {
	if ui := NewHuhUI(&bytes.Buffer{}, &bytes.Buffer{}); ui == nil {
		t.Fatal("NewHuhUI returned nil")
	}
}
