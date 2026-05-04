package tasks

import (
	"bytes"
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
