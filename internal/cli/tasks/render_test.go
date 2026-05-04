package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/store"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"zero", 0, "0m 0s"},
		{"sub-minute", 59 * time.Second, "0m 59s"},
		{"exact-minute", time.Minute, "1m 0s"},
		{"hours-roll-into-minutes", 90*time.Minute + 5*time.Second, "90m 5s"},
		{"negative-clamps-to-zero", -42 * time.Second, "0m 0s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatDuration(tc.in)
			if got != tc.want {
				t.Fatalf("formatDuration(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatStatus(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	begin := now.Add(-80 * time.Second) // 1m 20s ago

	activeCases := []struct {
		name   string
		status store.TaskStatus
		setter func(*store.Task, time.Time)
	}{
		{"planning", store.StatusPlanning, func(task *store.Task, t time.Time) { task.PlanBeginAt = &t }},
		{"working", store.StatusWorking, func(task *store.Task, t time.Time) { task.WorkBeginAt = &t }},
		{"verifying", store.StatusVerifying, func(task *store.Task, t time.Time) { task.VerifyBeginAt = &t }},
	}
	for _, tc := range activeCases {
		t.Run("active/"+tc.name, func(t *testing.T) {
			task := store.Task{Status: tc.status}
			tc.setter(&task, begin)
			got := formatStatus(task, now)
			want := string(tc.status) + " 1m 20s"
			if got != want {
				t.Fatalf("formatStatus = %q, want %q", got, want)
			}
		})
	}

	rawCases := []store.TaskStatus{
		store.StatusPlanDone, store.StatusWorkDone, store.StatusVerifyDone,
		store.StatusCompleted, store.StatusHelp,
	}
	for _, s := range rawCases {
		t.Run("raw/"+string(s), func(t *testing.T) {
			got := formatStatus(store.Task{Status: s}, now)
			if got != string(s) {
				t.Fatalf("formatStatus(%s) = %q, want %q", s, got, string(s))
			}
		})
	}

	// Active status with nil matching *BeginAt must fall back to the
	// raw status string instead of panicking on a nil deref.
	t.Run("active-without-begin-at", func(t *testing.T) {
		got := formatStatus(store.Task{Status: store.StatusPlanning}, now)
		if got != string(store.StatusPlanning) {
			t.Fatalf("formatStatus = %q, want %q", got, string(store.StatusPlanning))
		}
	})
}

func TestRenderTable_EmptyHeaderOnly(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTable(&buf, nil, time.Now()); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	out := buf.String()
	for _, glyph := range []string{"┌", "┐", "└", "┘", "│", "─"} {
		if !strings.Contains(out, glyph) {
			t.Fatalf("missing border glyph %q: %q", glyph, out)
		}
	}
	for _, header := range []string{"ID", "STATUS", "TOOL", "MODEL", "SUMMARY"} {
		if !strings.Contains(out, header) {
			t.Fatalf("missing header column %q: %q", header, out)
		}
	}
}

func TestRenderTable_MixedActiveAndInactive(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	begin := now.Add(-80 * time.Second)
	end := now.Add(-time.Hour)
	tasks := []store.Task{
		{
			ID:           "active-1",
			Status:       store.StatusPlanning,
			InvokedTool:  "cursor",
			InvokedModel: "sonnet-4",
			Summary:      "draft idea",
			PlanBeginAt:  &begin,
		},
		{
			ID:           "done-1",
			Status:       store.StatusPlanDone,
			InvokedTool:  "cursor",
			InvokedModel: "gpt-5",
			Summary:      "old one",
			PlanEndAt:    &end,
		},
	}
	var buf bytes.Buffer
	if err := renderTable(&buf, tasks, now); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "planning 1m 20s") {
		t.Fatalf("expected ticking active status: %q", out)
	}
	if !strings.Contains(out, "plan-done") {
		t.Fatalf("expected raw inactive status: %q", out)
	}
	if strings.Contains(out, "plan-done 0m") {
		t.Fatalf("inactive row should not be decorated: %q", out)
	}
	if !strings.Contains(out, "draft idea") || !strings.Contains(out, "old one") {
		t.Fatalf("expected summary cells: %q", out)
	}
}

func TestRenderTable_WriterError(t *testing.T) {
	if err := renderTable(failingWriter{}, []store.Task{
		{ID: "x", Status: store.StatusPlanDone},
	}, time.Now()); err == nil {
		t.Fatal("expected writer error from failingWriter")
	}
	if err := renderTable(failingWriter{}, nil, time.Now()); err == nil {
		t.Fatal("expected writer error on empty table too")
	}
}
