package tasks

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/spacelions/j/internal/store"
)

// Adaptive palette: light values target a white background, dark values
// target a black one. Orange paints the outer frame so the table reads
// as a single object; grey paints the inner gridlines so cell
// boundaries are visible without competing with content. The five
// rotating colours give each data row its own foreground so the eye
// can track active rows at a glance.
var (
	orangeColor = lipgloss.AdaptiveColor{Light: "#EA580C", Dark: "#FB923C"}
	greyColor   = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"}
	greenColor  = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#4ADE80"}
	purpleColor = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C084FC"}
	blueColor   = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#38BDF8"}

	rowPalette = []lipgloss.TerminalColor{
		greyColor, greenColor, purpleColor, blueColor, orangeColor,
	}

	outerBorderStyle = lipgloss.NewStyle().Foreground(orangeColor)
	innerSepStyle    = lipgloss.NewStyle().Foreground(greyColor)
	headerStyle      = lipgloss.NewStyle().Foreground(orangeColor).Bold(true)
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
func formatStatus(t store.Task, now time.Time) string {
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

// renderTable writes a bordered task table to w. Outer frame
// (corners, edges, walls) renders in thin orange; inner gridlines
// (column separators, inter-row separators, header separator) render
// in thin grey. Header text is orange-bold; each data row's cells
// rotate through `rowPalette`. width controls horizontal sizing:
// width <= 0 uses natural column widths; width > 0 fits to that many
// columns by shrinking the trailing SUMMARY column (truncation with
// `…` when needed). Writer errors propagate.
func renderTable(w io.Writer, tasks []store.Task, now time.Time, width int) error {
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
	b.WriteString(buildBorderLine(cols, "┌", "┬", "┐", outerBorderStyle, outerBorderStyle))
	b.WriteByte('\n')
	b.WriteString(buildContentLine(rows[0], cols, headerStyle))
	b.WriteByte('\n')
	for i, r := range rows[1:] {
		b.WriteString(buildBorderLine(cols, "├", "┼", "┤", outerBorderStyle, innerSepStyle))
		b.WriteByte('\n')
		rowStyle := lipgloss.NewStyle().Foreground(rowPalette[i%len(rowPalette)])
		b.WriteString(buildContentLine(r, cols, rowStyle))
		b.WriteByte('\n')
	}
	b.WriteString(buildBorderLine(cols, "└", "┴", "┘", outerBorderStyle, outerBorderStyle))
	b.WriteByte('\n')

	_, err := io.WriteString(w, b.String())
	return err
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
// widths: `left` and `right` corner glyphs, `mid` at every column
// junction, `─` filling each column span (including its 2-space
// padding). The corners and each mid junction render with
// `endpointStyle`; the `─` segments render with `fillStyle`. This is
// how the top/bottom borders end up fully orange while the inter-row
// separators inherit the orange-end / grey-fill split.
func buildBorderLine(cols []int, left, mid, right string, endpointStyle, fillStyle lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(endpointStyle.Render(left))
	for i, c := range cols {
		b.WriteString(fillStyle.Render(strings.Repeat("─", c+2)))
		if i < len(cols)-1 {
			b.WriteString(endpointStyle.Render(mid))
		}
	}
	b.WriteString(endpointStyle.Render(right))
	return b.String()
}

// buildContentLine emits one data (or header) row: orange outer
// walls, grey inner column separators, and content rendered with
// `cellStyle` (header: orange-bold; data: rotating palette colour).
// Each cell is space-padded to its column width.
func buildContentLine(cells []string, cols []int, cellStyle lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(outerBorderStyle.Render("│"))
	for i, c := range cells {
		pad := cols[i] - lipgloss.Width(c)
		b.WriteString(cellStyle.Render(" " + c + strings.Repeat(" ", pad) + " "))
		if i < len(cells)-1 {
			b.WriteString(innerSepStyle.Render("│"))
		}
	}
	b.WriteString(outerBorderStyle.Render("│"))
	return b.String()
}
