package tasks

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
