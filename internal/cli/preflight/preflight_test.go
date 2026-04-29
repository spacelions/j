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

	"github.com/spacelions/j/internal/store"
)

// scriptedUI is a deterministic UI fake that records prompt invocations
// and answers ConfirmInit with a pre-set boolean. The error field lets
// tests drive the "prompt errored" branch.
type scriptedUI struct {
	confirm bool
	err     error
	calls   int
}

func (u *scriptedUI) ConfirmInit(context.Context) (bool, error) {
	u.calls++
	if u.err != nil {
		return false, u.err
	}
	return u.confirm, nil
}

// TestEnsure_AlreadyInitialized pins the happy path: with all four
// artifacts present, Ensure returns nil without ever invoking the UI.
func TestEnsure_AlreadyInitialized(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	ui := &scriptedUI{}
	var stderr bytes.Buffer
	if err := Ensure(context.Background(), ui, &stderr); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if ui.calls != 0 {
		t.Fatalf("UI should not be prompted on initialized project: %d", ui.calls)
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
		{"listDB", filepath.Join(store.TasksDirName, store.TasksDBName)},
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
// an already-initialized cwd makes the helper return nil. The huh UI
// is constructed but never reached because Ensure short-circuits.
func TestPreRunE_Initialized(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatal(err)
	}
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
	cmd := &cobra.Command{}
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
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
