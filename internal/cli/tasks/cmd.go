// Package tasks implements the `j tasks` subcommand. It reads the
// per-project task log written by `j plan` and `j work` (the bbolt DB
// at `<cwd>/.j/tasks/list.db`) and prints a stable, human-readable
// list to stdout. No mutations are performed: editing, deleting, and
// resuming tasks are intentionally out of scope.
package tasks

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/store"
)

// emptyMessage is the single line printed to stdout when no task log
// exists yet, the bucket is missing, or the bucket is empty. Pinning it
// in a constant keeps the test assertion and the command output in
// lockstep.
const emptyMessage = "J: no tasks"

// simpleFlag is the long name of the bool that opts out of the
// bordered (and ticking, when on a TTY) renderer in favour of the
// plain tabwriter output preserved for pipes and scripts.
const simpleFlag = "simple"

// New returns the `j tasks` cobra command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "List planning/work tasks recorded in <cwd>/.j/tasks/",
		Long: "Reads the per-project task log written by `j plan` and " +
			"`j work` (bbolt at <cwd>/.j/tasks/list.db) and prints a " +
			"stable list to stdout. Active tasks (planning, working, " +
			"verifying, help) appear first; inactive tasks (plan-done, " +
			"work-done, verify-done, completed) follow, sorted by the " +
			"latest of their phase end timestamps. Each task is " +
			"rendered as a single summary row carrying id, status, " +
			"tool, model, and the human summary. Task bodies live as " +
			"files in <cwd>/.j/tasks/<id>/ (requirements.md, plan.md).",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			simple, _ := cmd.Flags().GetBool(simpleFlag)
			return listTasks(cmd.OutOrStdout(), simple)
		},
	}
	cmd.Flags().BoolP(simpleFlag, "s", false,
		"Print plain tabwriter output (no border, no ticking). Use for pipes and scripts.")
	cmd.AddCommand(newDiscardCmd())
	cmd.AddCommand(newEnterCmd())
	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newContinueCmd())
	cmd.AddCommand(newOrchestrateCmd())
	cmd.AddCommand(newRedoPlanCmd())
	cmd.AddCommand(newRedoWorkCmd())
	cmd.AddCommand(newRedoVerifyCmd())
	return cmd
}

// listTasks resolves the default tasks DB path, opens it, decodes
// every Task, sorts them via store.SortTasks, and dispatches to one
// of three renderers: the plain tabwriter output (--simple), the
// bubbletea TUI (default on a TTY), or a single bordered one-shot
// render (default off a TTY). Pre-flight guarantees the file exists
// before listTasks runs, but the missing-DB short-circuit is kept
// for defense in depth (e.g. a unit test that drives the function
// without going through the cobra wiring).
//
// Between ListTasks and SortTasks the helper reaps any background
// runs whose detached cursor-agent child has exited so the printed
// rows reflect fresh state. Reaping mutates the bbolt store
// (best-effort: PutTask errors are warned on stderr) and is opt-in
// per row: only entries with a non-zero BackgroundPID are touched.
func listTasks(stdout io.Writer, simple bool) error {
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
		banner.Fprintln(stdout, emptyMessage)
		return nil
	}
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	tasks, err := s.ListTasks()
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		banner.Fprintln(stdout, emptyMessage)
		return nil
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return err
	}
	tasks = reapBackgroundTasks(s, os.Stderr, tasksDir, tasks)
	store.SortTasks(tasks)
	if simple {
		return writeTasks(stdout, tasks)
	}
	if isTerminal(stdout) {
		return runWatch(os.Stdin, stdout, storeReloader(s, tasksDir))
	}
	return renderTable(stdout, tasks, time.Now(), terminalWidth(stdout))
}

// terminalWidth returns the column count of the terminal w is
// attached to, or 0 when w is not an *os.File or term.GetSize
// fails. The non-TTY one-shot path uses this so the bordered table
// fits the parent terminal width when its stdout is connected to a
// terminal (e.g. `j tasks` running interactively but with the watch
// path skipped); pure pipes/buffers fall back to natural widths.
func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return cols
}

// storeReloader returns a closure the bubbletea model uses to refresh
// its task slice on every tick. It re-runs ListTasks, reaps any
// background children that have since exited, and sorts the result so
// the rendered table reflects the latest state of the bbolt store.
func storeReloader(s *store.Store, tasksDir string) func() ([]store.Task, error) {
	return func() ([]store.Task, error) {
		t, err := s.ListTasks()
		if err != nil {
			return nil, err
		}
		t = reapBackgroundTasks(s, os.Stderr, tasksDir, t)
		store.SortTasks(t)
		return t, nil
	}
}

// isTerminal reports whether w is an *os.File pointing at an
// interactive terminal. cobra wires the command's stdout to *os.File
// in the production path; tests pass *bytes.Buffer or io.Discard so
// they reliably take the non-TTY path.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// writeTasks emits one summary line per task (tab-aligned via
// tabwriter) carrying id, status, tool, model, and the human summary.
// Per-phase resume cursors (plan / work / verify) are still kept on
// the underlying Task so `j plan resume` and `j work resume` can use
// them, but `j tasks` no longer surfaces them in the listing.
//
// tabwriter buffers writes internally and only surfaces underlying
// writer errors on Flush, so per-line Fprintln returns are
// intentionally not checked: they cannot fail in isolation.
func writeTasks(out io.Writer, tasks []store.Task) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tTOOL\tMODEL\tSUMMARY")
	for _, t := range tasks {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Status, t.InvokedTool, t.InvokedModel, t.Summary)
	}
	return tw.Flush()
}
