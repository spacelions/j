// Package banner renders shared terminal-facing text. It owns both the
// bordered, two-line announcement printed when a `j` subcommand forks
// the coding-agent into the background and the plain message text used
// for status, warning, empty-state, and completion lines.
// The banner is a square `lipgloss.NormalBorder()` box with the
// subject + PID on the first row, a blank row, and the `tail -f
// <agent-log>` invitation on the third row. The frame uses a neutral
// grey border; the subject row uses a violet accent (no bold); the
// tail row is sky-blue so the copy-paste command reads clearly.
// Centralising the render here keeps every fork site (`j tasks start`,
// `j plan`, `j work`) emitting the same shape so the user sees a
// consistent prompt — and lipgloss/termenv auto-strips the colour when
// stdout is not a TTY so pipes and tests still see clean text.
package banner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Adaptive palette: light values target a white background, dark
// values target a black one. The border is neutral grey; the subject
// row uses violet accent text; the tail row uses a cool blue for the
// copy-paste line. Plain message text intentionally reuses the neutral
// grey, dangerous text uses orange, and neither opts into bold styling.
var (
	borderColor = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"}
	dangerColor = lipgloss.AdaptiveColor{Light: "#EA580C", Dark: "#FB923C"}
	accentColor = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C084FC"}
	tailColor   = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#38BDF8"}

	textStyle    = lipgloss.NewStyle().Foreground(borderColor)
	dangerStyle  = lipgloss.NewStyle().Foreground(dangerColor)
	subjectStyle = lipgloss.NewStyle().Foreground(accentColor)
	tailStyle    = lipgloss.NewStyle().Foreground(tailColor)
	boxStyle     = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(borderColor).
			Padding(0, 1)
)

// Text renders s as plain grey terminal text.
func Text(s string) string {
	return renderText(textStyle, s)
}

// Fprint writes grey terminal text to w using fmt.Sprint semantics.
func Fprint(w io.Writer, a ...any) (int, error) {
	return fmt.Fprint(w, Text(fmt.Sprint(a...)))
}

// Fprintf writes formatted grey terminal text to w.
func Fprintf(w io.Writer, format string, a ...any) (int, error) {
	return fmt.Fprint(w, Text(fmt.Sprintf(format, a...)))
}

// Fprintln writes grey terminal text to w using fmt.Sprintln semantics.
func Fprintln(w io.Writer, a ...any) (int, error) {
	return fmt.Fprint(w, Text(fmt.Sprintln(a...)))
}

// DangerousText renders s as orange terminal text for warnings and destructive actions.
func DangerousText(s string) string {
	return renderText(dangerStyle, s)
}

// DangerousFprint writes orange terminal text to w using fmt.Sprint semantics.
func DangerousFprint(w io.Writer, a ...any) (int, error) {
	return fmt.Fprint(w, DangerousText(fmt.Sprint(a...)))
}

// DangerousFprintf writes formatted orange terminal text to w.
func DangerousFprintf(w io.Writer, format string, a ...any) (int, error) {
	return fmt.Fprint(w, DangerousText(fmt.Sprintf(format, a...)))
}

// DangerousFprintln writes orange terminal text to w using fmt.Sprintln semantics.
func DangerousFprintln(w io.Writer, a ...any) (int, error) {
	return fmt.Fprint(w, DangerousText(fmt.Sprintln(a...)))
}

func renderText(style lipgloss.Style, s string) string {
	if !strings.Contains(s, "\n") {
		return style.Render(s)
	}
	var b strings.Builder
	for _, part := range strings.SplitAfter(s, "\n") {
		if part == "" {
			continue
		}
		line := strings.TrimSuffix(part, "\n")
		b.WriteString(style.Render(line))
		if strings.HasSuffix(part, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// RunningInBackground writes the bordered background-fork banner to
// w. subject is the human-readable noun used in row one (e.g.
// "task <id>" or the coding-agent name). pid is the spawned child's
// PID. absLogPath is the absolute path of the per-task agent.log;
// the rendered second row prefers the cwd-relative form when the
// log lives under cwd, falling back to the absolute path otherwise.
func RunningInBackground(w io.Writer, subject string, pid int, absLogPath string) {
	block := strings.Join([]string{
		subjectStyle.Render(fmt.Sprintf("J: %s running in background (PID=%d)", subject, pid)),
		"",
		tailStyle.Render(fmt.Sprintf("tail -f %s", displayLogPath(absLogPath))),
	}, "\n")
	fmt.Fprintln(w, boxStyle.Render(block))
}

// displayLogPath returns the form of absLogPath shown to the user.
// When the path lives inside the process's cwd, the relative form is
// preferred (so the user can copy/paste the rendered `tail -f` line
// from the project root). When os.Getwd or filepath.Rel fail, or
// when the relative form escapes cwd via a leading `..`, the
// absolute path is returned instead so the line stays unambiguous.
func displayLogPath(absLogPath string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return absLogPath
	}
	rel, err := filepath.Rel(cwd, absLogPath)
	if err != nil {
		return absLogPath
	}
	if rel == "" || strings.HasPrefix(rel, "..") {
		return absLogPath
	}
	return rel
}
