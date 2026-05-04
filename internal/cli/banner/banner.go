// Package banner renders the bordered, two-line announcement printed
// when a `j` subcommand forks the coding-agent into the background.
// The banner is a square `lipgloss.NormalBorder()` box with the
// subject + PID on the first row, a blank row, and the `tail -f
// <agent-log>` invitation on the third row. Centralising the render
// here keeps every fork site (`j tasks start`, `j plan`, `j work`)
// emitting the same shape so the user sees a consistent prompt.
package banner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RunningInBackground writes the bordered background-fork banner to
// w. subject is the human-readable noun used in row one (e.g.
// "task <id>" or the coding-agent name). pid is the spawned child's
// PID. absLogPath is the absolute path of the per-task agent.log;
// the rendered second row prefers the cwd-relative form when the
// log lives under cwd, falling back to the absolute path otherwise.
func RunningInBackground(w io.Writer, subject string, pid int, absLogPath string) {
	block := strings.Join([]string{
		fmt.Sprintf("J: %s running in background (PID=%d)", subject, pid),
		"",
		fmt.Sprintf("tail -f %s", displayLogPath(absLogPath)),
	}, "\n")
	rendered := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(0, 1).
		Render(block)
	fmt.Fprintln(w, rendered)
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
