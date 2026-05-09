package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/cli/picker"
)

// TestSourcePicker_TaskLabelIsTask verifies the third source-picker
// option renders as "task" (not "existing task"). The Source type's
// raw string value is the picker label, so testing the constant
// directly is the definitive black‑box check.
func TestSourcePicker_TaskLabelIsTask(t *testing.T) {
	if string(picker.SourceTask) != "task" {
		t.Fatalf("SourceTask = %q, want %q",
			picker.SourceTask, "task")
	}
	if string(picker.SourceTask) == "existing task" {
		t.Fatal("SourceTask still has the old label 'existing task'")
	}
	if string(picker.SourceMarkdown) != "markdown" {
		t.Fatalf("SourceMarkdown = %q, want %q",
			picker.SourceMarkdown, "markdown")
	}
	if string(picker.SourceLinear) != "linear" {
		t.Fatalf("SourceLinear = %q, want %q",
			picker.SourceLinear, "linear")
	}
}
