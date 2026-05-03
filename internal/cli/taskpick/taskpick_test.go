package taskpick

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// TestFormatLabels_StatusAndSummary pins the label shape:
// "<id> — <status> — <summary>" and the byLabel reverse map round-
// trip. The label is the only user-facing string the helper
// produces, so this is the authoritative contract test.
func TestFormatLabels_StatusAndSummary(t *testing.T) {
	tasks := []store.Task{
		{ID: "01A", Status: store.StatusPlanDone, Summary: "first"},
		{ID: "01B", Status: store.StatusWorking, Summary: "second"},
	}
	labels, byLabel := FormatLabels(tasks)
	want := []string{
		"01A — plan-done — first",
		"01B — working — second",
	}
	if len(labels) != len(want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
	for i, got := range labels {
		if got != want[i] {
			t.Fatalf("labels[%d] = %q, want %q", i, got, want[i])
		}
	}
	for i, label := range labels {
		id, ok := byLabel[label]
		if !ok {
			t.Fatalf("byLabel[%q] missing", label)
		}
		if id != tasks[i].ID {
			t.Fatalf("byLabel[%q] = %q, want %q", label, id, tasks[i].ID)
		}
	}
}

// TestFormatLabels_EmptySummaryFallback covers the "(no summary)"
// fallback so an unsummarised row still renders as a selectable
// label (and a stray whitespace-only summary collapses to the
// same fallback rather than a blank tail).
func TestFormatLabels_EmptySummaryFallback(t *testing.T) {
	tasks := []store.Task{
		{ID: "01A", Status: store.StatusPlanning, Summary: ""},
		{ID: "01B", Status: store.StatusHelp, Summary: "   \t\n"},
	}
	labels, _ := FormatLabels(tasks)
	for _, got := range labels {
		if !strings.HasSuffix(got, "(no summary)") {
			t.Fatalf("label = %q, want trailing (no summary)", got)
		}
	}
}

// TestFormatLabels_EmptyInput pins the zero-input branch: callers
// should pre-screen but the helper is safe with an empty slice
// (no allocations beyond the empty backing arrays).
func TestFormatLabels_EmptyInput(t *testing.T) {
	labels, byLabel := FormatLabels(nil)
	if len(labels) != 0 {
		t.Fatalf("labels = %v, want empty", labels)
	}
	if len(byLabel) != 0 {
		t.Fatalf("byLabel = %v, want empty", byLabel)
	}
}

// TestPick_EmptyTasks asserts the "defensive" empty-input branch:
// callers should pre-screen but if a stray empty slice reaches
// Pick the helper collapses to ("", false, nil). No huh form is
// rendered, so the test is hermetic without a TTY.
func TestPick_EmptyTasks(t *testing.T) {
	id, ok, err := Pick(context.Background(), strings.NewReader(""), io.Discard, "Select a task", nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if ok {
		t.Fatal("ok = true, want false (empty input)")
	}
	if id != "" {
		t.Fatalf("id = %q, want empty", id)
	}
}
