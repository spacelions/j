package uitheme

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	tsk "github.com/spacelions/j/internal/store/tasks"
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

	borderStyle = lipgloss.NewStyle().Foreground(greyColor).Faint(true)
	headerStyle = lipgloss.NewStyle().Foreground(greyColor).Bold(true)
	doneStyle   = lipgloss.NewStyle().Foreground(greyColor)
	helpStyle   = lipgloss.NewStyle().Foreground(redColor)
)

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	mins := int(d / time.Minute)
	secs := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm:%ds", mins, secs)
}

func formatStatus(t tsk.Task, now time.Time) string {
	begin, ok := activeBeginAt(t)
	if !ok {
		return string(t.Status)
	}
	return fmt.Sprintf("%s(%s)", t.Status, formatDuration(now.Sub(begin)))
}

func activeBeginAt(t tsk.Task) (time.Time, bool) {
	var zero time.Time
	switch t.Status {
	case tsk.StatusPlanning:
		zero = t.PlanBeginAt
	case tsk.StatusWorking:
		zero = t.WorkBeginAt
	case tsk.StatusVerifying:
		zero = t.VerifyBeginAt
	default:
		return time.Time{}, false
	}
	if zero.IsZero() {
		return time.Time{}, false
	}
	return zero, true
}

// WriteTaskTable writes a bordered task table to w. The frame is a
// thin grey rounded box (`╭ ╮ ╰ ╯` corners, grey ─/│ edges and
// walls, grey `┬ ┴ ├ ┤ ┼` tees and intersections). The header is
// grey-bold.
// Active rows (planning / working / verifying / help) rotate through
// activeRowPalette so in-flight tasks pop; completed rows render in
// grey so they recede. width controls horizontal sizing: width <= 0
// uses natural column widths; width > 0 fits by shrinking the trailing
// SUMMARY column (truncation with `…` when needed). Writer errors
// propagate.
func WriteTaskTable(
	w io.Writer, taskRows []tsk.Task, now time.Time, width int,
) error {
	header := []string{"ID", "STATUS", "TOOL", "MODEL", "SUMMARY"}
	rows := make([][]string, 0, len(taskRows)+1)
	rows = append(rows, header)
	for _, t := range taskRows {
		tool, model := t.DisplayToolModel()
		rows = append(rows, []string{
			t.ID,
			formatStatus(t, now),
			tool,
			model,
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
		b.WriteString(buildContentLine(r, cols, rowStyle(taskRows[i], &activeIdx)))
		b.WriteByte('\n')
	}
	b.WriteString(buildBorderLine(cols, "╰", "┴", "╯"))
	b.WriteByte('\n')

	_, err := io.WriteString(w, b.String())
	return err
}

func rowStyle(t tsk.Task, activeIdx *int) lipgloss.Style {
	switch t.Status {
	case tsk.StatusCompleted:
		return doneStyle
	case tsk.StatusHelp:
		return helpStyle
	default:
	}
	style := lipgloss.NewStyle().Foreground(
		activeRowPalette[*activeIdx%len(activeRowPalette)])
	*activeIdx++
	return style
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

func tableTotalWidth(cols []int) int {
	total := 0
	for _, c := range cols {
		total += c + 2
	}
	total += len(cols) + 1
	return total
}

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

func truncateCell(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= limit {
		return s
	}
	if limit == 1 {
		return "…"
	}
	runes := []rune(s)
	cut := runes[:limit-1]
	for lipgloss.Width(string(cut))+1 > limit && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return string(cut) + "…"
}

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

func buildContentLine(
	cells []string, cols []int, cellStyle lipgloss.Style,
) string {
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
