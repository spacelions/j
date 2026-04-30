package tasks

import (
	"bytes"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// TestNewHuhUI_NotNil pins the constructor: it returns a non-nil
// huh-backed UI implementation. Driving the form requires a TTY so
// the body of ConfirmDelete is exercised through the UI interface
// in the orchestration tests in delete_test.go.
func TestNewHuhUI_NotNil(t *testing.T) {
	if u := newHuhUI(&bytes.Buffer{}, &bytes.Buffer{}); u == nil {
		t.Fatal("newHuhUI returned nil")
	}
}

// TestFormatEnterLabels pins the label shape for the enter / delete
// picker: `<id> — <status> — <summary>` with a `(no summary)`
// fallback when Summary is empty / whitespace, and a label-to-id
// reverse map keyed by every emitted label.
func TestFormatEnterLabels(t *testing.T) {
	tasks := []store.Task{
		{ID: "id-1", Status: store.StatusPlanning, Summary: "draft idea"},
		{ID: "id-2", Status: store.StatusPlanDone, Summary: ""},
		{ID: "id-3", Status: store.StatusWorkDone, Summary: "   "},
	}
	labels, byLabel := formatEnterLabels(tasks)
	if len(labels) != 3 {
		t.Fatalf("labels = %d, want 3", len(labels))
	}
	wantOrder := []string{
		"id-1 — planning — draft idea",
		"id-2 — plan-done — (no summary)",
		"id-3 — work-done — (no summary)",
	}
	for i, want := range wantOrder {
		if labels[i] != want {
			t.Fatalf("labels[%d] = %q, want %q", i, labels[i], want)
		}
		if byLabel[want] != tasks[i].ID {
			t.Fatalf("byLabel[%q] = %q, want %q", want, byLabel[want], tasks[i].ID)
		}
	}
}

// TestFormatEnterLabels_Empty pins the no-tasks branch: the helper
// returns non-nil but empty slices and maps so callers can still
// range over them safely.
func TestFormatEnterLabels_Empty(t *testing.T) {
	labels, byLabel := formatEnterLabels(nil)
	if labels == nil || len(labels) != 0 {
		t.Fatalf("labels = %v, want empty non-nil slice", labels)
	}
	if byLabel == nil || len(byLabel) != 0 {
		t.Fatalf("byLabel = %v, want empty non-nil map", byLabel)
	}
}
