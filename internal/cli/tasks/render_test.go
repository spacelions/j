package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/spacelions/j/internal/store"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"zero", 0, "0m:0s"},
		{"sub-minute", 59 * time.Second, "0m:59s"},
		{"exact-minute", time.Minute, "1m:0s"},
		{"hours-roll-into-minutes", 90*time.Minute + 5*time.Second, "90m:5s"},
		{"negative-clamps-to-zero", -42 * time.Second, "0m:0s"},
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
			want := string(tc.status) + "(1m:20s)"
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
	if err := renderTable(&buf, nil, time.Now(), 0); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	out := buf.String()
	for _, glyph := range []string{"╭", "╮", "╰", "╯", "│", "─"} {
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

// TestRenderTable_GlyphTopology pins the gridline shape: rounded
// corners (`╭ ╮ ╰ ╯`), inter-row separators with `┼` intersections,
// and column-tee glyphs (`┬ ┴ ├ ┤`) must all surface so a future
// regression can't silently flatten the table back to header-only
// ruling.
func TestRenderTable_GlyphTopology(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	tasks := []store.Task{
		{ID: "a", Status: store.StatusPlanDone, Summary: "first"},
		{ID: "b", Status: store.StatusWorkDone, Summary: "second"},
	}
	var buf bytes.Buffer
	if err := renderTable(&buf, tasks, now, 0); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	out := ansi.Strip(buf.String())
	for _, glyph := range []string{"╭", "╮", "╰", "╯", "├", "┤", "┬", "┴", "┼", "│", "─"} {
		if !strings.Contains(out, glyph) {
			t.Fatalf("missing border glyph %q in stripped output: %q", glyph, out)
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
	if err := renderTable(&buf, tasks, now, 0); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "planning(1m:20s)") {
		t.Fatalf("expected ticking active status: %q", out)
	}
	if !strings.Contains(out, "plan-done") {
		t.Fatalf("expected raw inactive status: %q", out)
	}
	if strings.Contains(out, "plan-done(") {
		t.Fatalf("inactive row should not be decorated: %q", out)
	}
	if !strings.Contains(out, "draft idea") || !strings.Contains(out, "old one") {
		t.Fatalf("expected summary cells: %q", out)
	}
}

// TestRenderTable_RowColors pins the rotation rule:
//   - `completed` rows are grey (they fade into the chrome),
//   - `help` rows are red (a stuck task should be impossible to miss),
//   - every other status — including the phase-done intermediates and
//     the in-flight states — rotates through the purple/blue/green/
//     orange palette.
// We seed live → completed → help → live to prove neither special
// status burns a palette slot.
func TestRenderTable_RowColors(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tasks := []store.Task{
		{ID: "row-1", Status: store.StatusPlanDone, Summary: "first"},
		{ID: "row-2", Status: store.StatusCompleted, Summary: "done"},
		{ID: "row-3", Status: store.StatusHelp, Summary: "stuck"},
		{ID: "row-4", Status: store.StatusWorkDone, Summary: "second"},
	}
	var buf bytes.Buffer
	if err := renderTable(&buf, tasks, now, 0); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	raw := buf.String()
	if ansi.Strip(raw) == raw {
		t.Skip("renderer stripped ANSI (no colour profile in test env); rotation cannot be observed")
	}
	expectations := []struct {
		text  string
		color lipgloss.TerminalColor
		why   string
	}{
		{"first", purpleColor, "first live row should render in purple"},
		{"done", greyColor, "completed row should render in grey"},
		{"stuck", redColor, "help row should render in red"},
		{"second", blueColor, "second live row should render in blue (active-palette[1])"},
	}
	for _, exp := range expectations {
		sample := lipgloss.NewStyle().Foreground(exp.color).Render(exp.text)
		if !strings.Contains(raw, sample) {
			t.Fatalf("%s; got %q", exp.why, raw)
		}
	}
}

// TestRenderTable_FitsToWidth verifies the trailing SUMMARY column
// truncates with `…` and every output line stays within the requested
// terminal width.
func TestRenderTable_FitsToWidth(t *testing.T) {
	tasks := []store.Task{
		{
			ID:           "row-1",
			Status:       store.StatusPlanDone,
			InvokedTool:  "cursor",
			InvokedModel: "sonnet-4",
			Summary:      "this is a very long summary that absolutely should not fit",
		},
	}
	const width = 50
	var buf bytes.Buffer
	if err := renderTable(&buf, tasks, time.Now(), width); err != nil {
		t.Fatalf("renderTable: %v", err)
	}
	stripped := ansi.Strip(buf.String())
	for _, line := range strings.Split(strings.TrimRight(stripped, "\n"), "\n") {
		if lipgloss.Width(line) > width {
			t.Fatalf("line %q exceeds width %d (got %d)", line, width, lipgloss.Width(line))
		}
	}
	if !strings.Contains(stripped, "…") {
		t.Fatalf("expected SUMMARY truncation indicator `…` in narrow render: %q", stripped)
	}
}

func TestRenderTable_WriterError(t *testing.T) {
	if err := renderTable(failingWriter{}, []store.Task{
		{ID: "x", Status: store.StatusPlanDone},
	}, time.Now(), 0); err == nil {
		t.Fatal("expected writer error from failingWriter")
	}
	if err := renderTable(failingWriter{}, nil, time.Now(), 0); err == nil {
		t.Fatal("expected writer error on empty table too")
	}
}

func TestTruncateCell(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"", 0, ""},
		{"hello", -1, ""},
		{"hello", 1, "…"},
		{"hello", 5, "hello"},
		{"helloworld", 6, "hello…"},
	}
	for _, tc := range cases {
		got := truncateCell(tc.in, tc.max)
		if got != tc.want {
			t.Fatalf("truncateCell(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
	// Wide East-Asian runes report width 2; the trim loop must drop
	// extra runes until the result + ellipsis fits, even when the
	// naive `runes[:max-1]` overshoots.
	if got := truncateCell("中文测试", 4); lipgloss.Width(got) > 4 {
		t.Fatalf("wide-rune truncate overshot width 4: %q (width %d)", got, lipgloss.Width(got))
	}
}

func TestFitToWidth_NoChangeWhenAvailableUnknownOrAmple(t *testing.T) {
	cols := []int{10, 10, 10}
	if got := fitToWidth(cols, 0); &got[0] != &cols[0] {
		t.Fatal("zero/negative available should return the same slice unchanged")
	}
	if got := fitToWidth(cols, 1000); &got[0] != &cols[0] {
		t.Fatal("ample width should return the same slice unchanged")
	}
}

func TestFitToWidth_ShrinksLastColumnToOne(t *testing.T) {
	cols := []int{20, 20, 20}
	got := fitToWidth(cols, 10) // far smaller than total
	if got[len(got)-1] != 1 {
		t.Fatalf("trailing column should clamp to 1 when available is tiny, got %v", got)
	}
}
