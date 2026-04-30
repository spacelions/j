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

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/store"
)

// emptyMessage is the single line printed to stdout when no task log
// exists yet, the bucket is missing, or the bucket is empty. Pinning it
// in a constant keeps the test assertion and the command output in
// lockstep.
const emptyMessage = "J: no tasks"

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
			return listTasks(cmd.OutOrStdout())
		},
	}
	cmd.AddCommand(newDeleteCmd())
	return cmd
}

// listTasks resolves the default tasks DB path, opens it, decodes
// every Task, sorts them via store.SortTasks, and writes the multi-
// line per-task block. Pre-flight guarantees the file exists before
// listTasks runs, but the missing-DB short-circuit is kept for
// defense in depth (e.g. a unit test that drives the function
// without going through the cobra wiring).
//
// Between ListTasks and SortTasks the helper reaps any background
// runs whose detached cursor-agent child has exited so the printed
// rows reflect fresh state. Reaping mutates the bbolt store
// (best-effort: PutTask errors are warned on stderr) and is opt-in
// per row: only entries with a non-zero BackgroundPID are touched.
func listTasks(stdout io.Writer) error {
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
		fmt.Fprintln(stdout, emptyMessage)
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
		fmt.Fprintln(stdout, emptyMessage)
		return nil
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		return err
	}
	tasks = reapBackgroundTasks(s, os.Stderr, tasksDir, tasks)
	store.SortTasks(tasks)
	return writeTasks(stdout, tasks)
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

// formatSession renders a single indented session line of the form
// "  <label>: <id>" where id falls back to "-" when empty. The
// helper is no longer used by `j tasks` itself but is retained as a
// package-private utility so resume-side selectors can reuse the
// same label shape if they need to surface session ids.
func formatSession(label, id string) string {
	if id == "" {
		id = "-"
	}
	return fmt.Sprintf("  %s: %s", label, id)
}
