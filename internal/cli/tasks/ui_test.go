package tasks

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	storeTasksPkg "github.com/spacelions/j/internal/store/tasks"
)

// TestNewHuhUI_NotNil pins the constructor: it returns a non-nil
// huh-backed UI implementation. Driving the form requires a TTY so
// the body of ConfirmDiscard is exercised through the UI interface
// in the orchestration tests in discard_test.go. Label-format and
// picker-contract assertions live alongside the shared widget in
// internal/cli/picker; the per-package picker delegate is just
// a one-line passthrough.
func TestNewHuhUI_NotNil(t *testing.T) {
	if u := newHuhUI(&bytes.Buffer{}, &bytes.Buffer{}); u == nil {
		t.Fatal("newHuhUI returned nil")
	}
}

// TestHuhUI_ConfirmStatusOverride_DelegatesToPicker pins the
// passthrough body: a cancelled ctx surfaces an error from the huh
// form (the picker wraps non-abort errors with "ui: "), proving the
// huhUI shim actually delegates to the picker leaf. The huh form
// itself requires a TTY so we cannot drive the happy path here; the
// happy path is exercised through the RePlanUI interface using a
// scripted fake in re_plan_test.go.
func TestHuhUI_ConfirmStatusOverride_DelegatesToPicker(t *testing.T) {
	u := newHuhUI(strings.NewReader(""), &bytes.Buffer{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := u.ConfirmStatusOverride(ctx, "re-plan", "abc", "working")
	if err == nil {
		t.Fatal("want error from cancelled ctx; the huhUI shim should propagate the picker leaf's error verbatim")
	}
}

// TestHuhUI_ConfirmDiscard_Yes drives ConfirmDiscard in accessible mode.
func TestHuhUI_ConfirmDiscard_Yes(t *testing.T) {
	t.Setenv("TERM", "dumb")
	task := storeTasksPkg.Task{ID: "T1", Summary: "do the thing"}
	u := newHuhUI(strings.NewReader("y\n"), io.Discard)
	got, err := u.ConfirmDiscard(t.Context(), task)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !got {
		t.Fatal("expected true for y input")
	}
}

// TestHuhUI_ConfirmDiscard_No drives the no branch.
func TestHuhUI_ConfirmDiscard_No(t *testing.T) {
	t.Setenv("TERM", "dumb")
	task := storeTasksPkg.Task{ID: "T1", Summary: "do the thing"}
	u := newHuhUI(strings.NewReader("n\n"), io.Discard)
	got, err := u.ConfirmDiscard(t.Context(), task)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got {
		t.Fatal("expected false for n input")
	}
}

// TestHuhUI_PickTask_HappyPath drives PickTask in accessible mode.
func TestHuhUI_PickTask_HappyPath(t *testing.T) {
	t.Setenv("TERM", "dumb")
	rows := []storeTasksPkg.Task{
		{ID: "T1", Status: storeTasksPkg.StatusPlanning, Summary: "first"},
		{ID: "T2", Status: storeTasksPkg.StatusPlanDone, Summary: "second"},
	}
	u := newHuhUI(strings.NewReader("\n"), io.Discard)
	id, ok, err := u.PickTask(t.Context(), rows)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !ok {
		t.Fatal("ok = false")
	}
	if id != "T1" {
		t.Fatalf("id = %q, want T1", id)
	}
}

// TestHuhUI_PickTask_Empty covers the empty-tasks short-circuit.
func TestHuhUI_PickTask_Empty(t *testing.T) {
	u := newHuhUI(strings.NewReader(""), io.Discard)
	id, ok, err := u.PickTask(t.Context(), nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok || id != "" {
		t.Fatalf("id=%q ok=%v, want empty+false", id, ok)
	}
}
