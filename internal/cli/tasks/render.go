package tasks

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/spacelions/j/internal/store/tasks"
)

// Adaptive palette: light values target a white background, dark values
// target a black one. Grey paints the entire frame (outer rounded
// border + inner gridlines) so the chrome stays calm; the four
// rotating colours give each *active* data row a distinct foreground
// so in-flight work pops, while completed rows stay grey to fade into
// the chrome.
var (
	orangeColor = lipgloss.AdaptiveColor{Light: "#EA580C", Dark: "#FB923C"}
	greyColor   = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"}
	greenColor  = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	purpleColor = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C084FC"}
	blueColor   = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#38BDF8"}
	redColor    = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}

	activeRowPalette = []lipgloss.TerminalColor{
		purpleColor, blueColor, greenColor, orangeColor,
	}

	// borderStyle uses .Faint(true) so the chrome renders at half
	// intensity in terminals that honour SGR 2 — the box-drawing
	// glyphs are already the thinnest single-line set Unicode offers,
	// and dimming is the only way to make them look lighter still.
	borderStyle = lipgloss.NewStyle().Foreground(greyColor).Faint(true)
	headerStyle = lipgloss.NewStyle().Foreground(greyColor).Bold(true)
	doneStyle   = lipgloss.NewStyle().Foreground(greyColor)
	helpStyle   = lipgloss.NewStyle().Foreground(redColor)
)

// formatDuration always renders d in `<minutes>m:<seconds>s` form.
// Negative durations clamp to "0m:0s"; hours roll into minutes (so 90
// minutes renders "90m:0s") so the field width stays predictable for
// the ticking column even after a long-running task.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	mins := int(d / time.Minute)
	secs := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm:%ds", mins, secs)
}

// formatStatus renders a task's STATUS column. Active rows
// (planning / working / verifying) with a non-nil matching *BeginAt
// get a parenthesised elapsed time, e.g. "planning(1m:20s)"; help and
// inactive states return the raw status string. now is injected so
// the renderer is fully pure (the TUI passes the latest tick; the
// non-TTY one-shot passes time.Now()).
func formatStatus(t tasks.Task, now time.Time) string {
	begin := activeBeginAt(t)
	if begin == nil {
		return string(t.Status)
	}
	return fmt.Sprintf("%s(%s)", t.Status, formatDuration(now.Sub(*begin)))
}

// activeBeginAt picks the *BeginAt that pairs with the active status,
// or nil for help/inactive rows. Help is intentionally excluded: it
// has no per-phase begin timestamp of its own, and the user-visible
// row is "the task is stuck" rather than "the task is running".
func activeBeginAt(t tasks.Task) *time.Time {
	switch t.Status {
	case tasks.StatusPlanning:
		return t.PlanBeginAt
	case tasks.StatusWorking:
		return t.WorkBeginAt
	case tasks.StatusVerifying:
		return t.VerifyBeginAt
	}
	return nil
}

// renderTable writes a bordered task table to w. The frame is a thin
// grey rounded box (`╭ ╮ ╰ ╯` corners, grey ─/│ edges and walls, grey
// `┬ ┴ ├ ┤ ┼` tees and intersections). The header is grey-bold.
// Active rows (planning / working / verifying / help) rotate through
// `activeRowPalette` so in-flight tasks pop; completed rows render in
// grey so they recede. width controls horizontal sizing: width <= 0
// uses natural column widths; width > 0 fits by shrinking the trailing
// SUMMARY column (truncation with `…` when needed). Writer errors
// propagate.
func renderTable(w io.Writer, tasks []tasks.Task, now time.Time, width int) error {
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
	cols := fitToWidth(columnWidths(rows), width)
	rows = applyColumnWidths(rows, cols)

	var b strings.Builder
	b.WriteString(buildBorderLine(cols, "╭", "┬", "╮"))
	b.WriteByte('\n')
	b.WriteString(buildContentLine(rows[0], cols, headerStyle))
	b.WriteByte('\n')
	activeIdx := 0
	for i, r := range rows[1:] {
		b.WriteString(buildBorderLine(cols, "├", "┼", "┤"))
		b.WriteByte('\n')
		b.WriteString(buildContentLine(r, cols, rowStyle(tasks[i], &activeIdx)))
		b.WriteByte('\n')
	}
	b.WriteString(buildBorderLine(cols, "╰", "┴", "╯"))
	b.WriteByte('\n')

	_, err := io.WriteString(w, b.String())
	return err
}

// rowStyle picks the foreground for a data row:
//   - `completed` rows take the grey `doneStyle` so they recede into
//     the chrome (those tasks are truly done).
//   - `help` rows take the red `helpStyle` so a stuck task is
//     impossible to miss.
//   - Every other status (planning / working / verifying and the
//     phase-done intermediates) rotates through `activeRowPalette`.
// activeIdx is bumped only on rotating rows so a completed/help row
// sandwiched between two live rows doesn't waste a palette slot.
func rowStyle(t tasks.Task, activeIdx *int) lipgloss.Style {
	switch t.Status {
	case tasks.StatusCompleted:
		return doneStyle
	case tasks.StatusHelp:
		return helpStyle
	}
	style := lipgloss.NewStyle().Foreground(activeRowPalette[*activeIdx%len(activeRowPalette)])
	*activeIdx++
	return style
}

// columnWidths returns the natural per-column width (largest cell)
// across header + data rows.
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

// tableTotalWidth returns how wide the rendered table is for the
// given column widths: padding (1 space each side per cell) +
// separator glyphs (one between each pair of cells, plus the outer
// walls) + cell content.
func tableTotalWidth(cols []int) int {
	total := 0
	for _, c := range cols {
		total += c + 2 // padding spaces on either side
	}
	total += len(cols) + 1 // outer walls + inner column separators
	return total
}

// fitToWidth shrinks the trailing column so the rendered table fits
// into `available` columns. `available <= 0` (terminal width unknown)
// or already-fitting widths return the slice unchanged. The minimum
// effective column width is 1 so truncation always leaves room for a
// `…` indicator.
func fitToWidth(cols []int, available int) []int {
	if available <= 0 {
		return cols
	}
	if tableTotalWidth(cols) <= available {
		return cols
	}
	out := append([]int(nil), cols...)
	excess := tableTotalWidth(out) - available
	last := len(out) - 1
	if out[last]-excess < 1 {
		out[last] = 1
	} else {
		out[last] -= excess
	}
	return out
}

// applyColumnWidths truncates any cell wider than its column to fit,
// appending a `…` when content is dropped. Cells already within the
// width are returned unchanged.
func applyColumnWidths(rows [][]string, cols []int) [][]string {
	out := make([][]string, len(rows))
	for r, row := range rows {
		nr := make([]string, len(row))
		for i, c := range row {
			nr[i] = truncateCell(c, cols[i])
		}
		out[r] = nr
	}
	return out
}

// truncateCell returns s if it already fits in `max` display
// columns; otherwise it returns the leading runes plus `…`. `max <= 0`
// returns the empty string; `max == 1` returns just `…`.
func truncateCell(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	runes := []rune(s)
	cut := runes[:max-1]
	for lipgloss.Width(string(cut))+1 > max && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return string(cut) + "…"
}

// buildBorderLine draws a horizontal frame line for the given column
// widths: `left` and `right` corner glyphs (rounded `╭ ╮ ╰ ╯` for the
// top/bottom rows, `├ ┤` for inter-row separators), `mid` at every
// column junction (`┬ ┼ ┴`), and `─` filling each column span
// (including its 2-space padding). All glyphs render with the single
// grey `borderStyle`.
func buildBorderLine(cols []int, left, mid, right string) string {
	var b strings.Builder
	b.WriteString(borderStyle.Render(left))
	for i, c := range cols {
		b.WriteString(borderStyle.Render(strings.Repeat("─", c+2)))
		if i < len(cols)-1 {
			b.WriteString(borderStyle.Render(mid))
		}
	}
	b.WriteString(borderStyle.Render(right))
	return b.String()
}

// buildContentLine emits one data (or header) row with grey walls and
// inner column separators; cell content is rendered with `cellStyle`
// (header: grey-bold; data: rotating active palette colour or grey
// done-style). Each cell is space-padded to its column width.
func buildContentLine(cells []string, cols []int, cellStyle lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(borderStyle.Render("│"))
	for i, c := range cells {
		pad := cols[i] - lipgloss.Width(c)
		b.WriteString(cellStyle.Render(" " + c + strings.Repeat(" ", pad) + " "))
		if i < len(cells)-1 {
			b.WriteString(borderStyle.Render("│"))
		}
	}
	b.WriteString(borderStyle.Render("│"))
	return b.String()
}
