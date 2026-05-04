// Package banner renders the bordered, two-line announcement printed
// when a `j` subcommand forks the coding-agent into the background.
// The banner is a square `lipgloss.NormalBorder()` box with the
// subject + PID on the first row, a blank row, and the `tail -f
// <agent-log>` invitation on the third row. Border + subject row
// share a violet accent; the tail row is sky-blue so the copy-paste
// command pops without competing with the headline. Centralising
// the render here keeps every fork site (`j tasks start`, `j plan`,
// `j work`) emitting the same shape so the user sees a consistent
// prompt — and lipgloss/termenv auto-strips the colour when stdout
// is not a TTY so pipes and tests still see clean text.
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
// values target a black one. Border + subject share an accent so
// the headline and the frame read as one element; the tail row uses
// a complementary cool tone to invite the eye to the copy-paste
// command without screaming for attention.
var (
	accentColor = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C084FC"}
	tailColor   = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#38BDF8"}

	subjectStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	tailStyle    = lipgloss.NewStyle().Foreground(tailColor)
	boxStyle     = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(accentColor).
			Padding(0, 1)
)

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
