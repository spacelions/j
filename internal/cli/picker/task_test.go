package picker

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

func TestFormatTaskLabels_StatusAndSummary(t *testing.T) {
	rows := []tasks.Task{
		{ID: "01A", Status: tasks.StatusPlanDone, Summary: "first"},
		{ID: "01B", Status: tasks.StatusWorking, Summary: "second"},
	}
	labels, byLabel := FormatTaskLabels(rows)
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
		if id != rows[i].ID {
			t.Fatalf("byLabel[%q] = %q, want %q", label, id, rows[i].ID)
		}
	}
}

func TestFormatTaskLabels_EmptySummaryFallback(t *testing.T) {
	rows := []tasks.Task{
		{ID: "01A", Status: tasks.StatusPlanning, Summary: ""},
		{ID: "01B", Status: tasks.StatusHelp, Summary: "   \t\n"},
	}
	labels, _ := FormatTaskLabels(rows)
	for _, got := range labels {
		if !strings.HasSuffix(got, "(no summary)") {
			t.Fatalf("label = %q, want trailing (no summary)", got)
		}
	}
}

func TestFormatTaskLabels_EmptyInput(t *testing.T) {
	labels, byLabel := FormatTaskLabels(nil)
	if len(labels) != 0 {
		t.Fatalf("labels = %v, want empty", labels)
	}
	if len(byLabel) != 0 {
		t.Fatalf("byLabel = %v, want empty", byLabel)
	}
}

func TestPickTask_EmptyTasks(t *testing.T) {
	p := New(strings.NewReader(""), io.Discard)
	id, ok, err := p.PickTask(context.Background(), "Select a task", nil)
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
