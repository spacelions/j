package tasks

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/spacelions/j/internal/store"
)

// tableBorderStyle wraps the rendered tasks table in a single-line
// box. lipgloss strips ANSI escapes when the writer isn't a TTY, so
// the border glyphs survive into pipes and tests as plain Unicode.
var tableBorderStyle = lipgloss.NewStyle().
	Border(lipgloss.NormalBorder()).
	Padding(0, 1)

// formatDuration always renders d in `<minutes>m <seconds>s` form.
// Negative durations clamp to "0m 0s"; hours roll into minutes (so 90
// minutes renders "90m 0s") so the field width stays predictable for
// the ticking column even after a long-running task.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	mins := int(d / time.Minute)
	secs := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm %ds", mins, secs)
}

// formatStatus renders a task's STATUS column. Active rows
// (planning / working / verifying) with a non-nil matching *BeginAt
// get a trailing elapsed time; help and inactive states return the
// raw status string. now is injected so the renderer is fully pure
// (the TUI passes the latest tick; the non-TTY one-shot passes
// time.Now()).
func formatStatus(t store.Task, now time.Time) string {
	begin := activeBeginAt(t)
	if begin == nil {
		return string(t.Status)
	}
	return fmt.Sprintf("%s %s", t.Status, formatDuration(now.Sub(*begin)))
}

// activeBeginAt picks the *BeginAt that pairs with the active status,
// or nil for help/inactive rows. Help is intentionally excluded: it
// has no per-phase begin timestamp of its own, and the user-visible
// row is "the task is stuck" rather than "the task is running".
func activeBeginAt(t store.Task) *time.Time {
	switch t.Status {
	case store.StatusPlanning:
		return t.PlanBeginAt
	case store.StatusWorking:
		return t.WorkBeginAt
	case store.StatusVerifying:
		return t.VerifyBeginAt
	}
	return nil
}

// renderTable writes a bordered table of tasks (header + rows) to w.
// Columns are space-padded to the widest cell so the box stays
// rectangular, and a horizontal `─` separator sits between the
// header and the data rows. Writer errors are surfaced verbatim so
// callers can react.
func renderTable(w io.Writer, tasks []store.Task, now time.Time) error {
	header := []string{"ID", "STATUS", "TOOL", "MODEL", "SUMMARY"}
	rows := make([][]string, 0, len(tasks)+1)
	rows = append(rows, header)
	for _, t := range tasks {
		rows = append(rows, []string{
			t.ID,
			formatStatus(t, now),
			t.InvokedTool,
			t.InvokedModel,
			t.Summary,
		})
	}
	widths := columnWidths(rows)
	var body strings.Builder
	for i, r := range rows {
		writeTableRow(&body, r, widths)
		if i == 0 {
			writeTableSeparator(&body, widths)
		}
	}
	rendered := tableBorderStyle.Render(strings.TrimRight(body.String(), "\n"))
	_, err := fmt.Fprintln(w, rendered)
	return err
}

func columnWidths(rows [][]string) []int {
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if l := lipgloss.Width(c); l > widths[i] {
				widths[i] = l
			}
		}
	}
	return widths
}

const tableColumnGap = "  "

func writeTableRow(b *strings.Builder, cells []string, widths []int) {
	for i, c := range cells {
		if i > 0 {
			b.WriteString(tableColumnGap)
		}
		b.WriteString(c)
		if i < len(cells)-1 {
			b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(c)))
		}
	}
	b.WriteByte('\n')
}

func writeTableSeparator(b *strings.Builder, widths []int) {
	total := 0
	for i, w := range widths {
		total += w
		if i > 0 {
			total += len(tableColumnGap)
		}
	}
	b.WriteString(strings.Repeat("─", total))
	b.WriteByte('\n')
}
