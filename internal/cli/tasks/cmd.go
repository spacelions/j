// Package tasks implements the `j tasks` subcommand. It reads the
// per-project task log written by `j plan` and `j work` (the bbolt DB
// at `<cwd>/.j/tasks/index.db`) and prints a stable, human-readable
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

	"github.com/spacelions/j/internal/store"
)

// emptyMessage is the single line printed to stdout when no task log
// exists yet, the bucket is missing, or the bucket is empty. Pinning it
// in a constant keeps the test assertion and the command output in
// lockstep.
const emptyMessage = "no tasks recorded"

// New returns the `j tasks` cobra command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "tasks",
		Short: "List planning/work tasks recorded in <cwd>/.j/tasks/",
		Long: "Reads the per-project task log written by `j plan` and " +
			"`j work` (bbolt at <cwd>/.j/tasks/index.db) and prints a " +
			"stable list to stdout. Active tasks (planning, working, " +
			"verifying, help) appear first; inactive tasks (plan-done, " +
			"work-done, verify-done, completed) follow, sorted by the " +
			"latest of their phase end timestamps. Each task is " +
			"rendered as one summary row followed by three indented " +
			"lines showing the per-phase resume sessions: `plan " +
			"session`, `work session`, and `verify session`. Empty " +
			"sessions show a dash. Resume a Cursor session with " +
			"`cursor-agent --resume <id>` using the id printed here. " +
			"Task bodies live as files in <cwd>/.j/tasks/<id>/ " +
			"(requirements.md, plan.md).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listTasks(cmd.OutOrStdout())
		},
	}
}

// listTasks resolves the default tasks DB path, opens it, decodes
// every Task, sorts them via store.SortTasks, and writes the multi-
// line per-task block. Missing DB or empty bucket prints emptyMessage.
// A non-NotExist stat failure (e.g. permission denied on the parent
// dir) propagates through bolt.Open below as a wrapped open error.
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
	store.SortTasks(tasks)
	return writeTasks(stdout, tasks)
}

// writeTasks emits one summary line per task (tab-aligned via
// tabwriter) plus three indented session lines so consumers can see
// every per-phase resume id without widening the table. The summary
// line carries id, status, tool, model, and the human summary; the
// session lines carry the plan / work / verify cursor (or a dash).
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
		fmt.Fprintln(tw, formatSession("plan session", t.PlanResumeCursor))
		fmt.Fprintln(tw, formatSession("work session", t.WorkResumeCursor))
		fmt.Fprintln(tw, formatSession("verify session", t.VerifyResumeCursor))
	}
	return tw.Flush()
}

// formatSession renders a single indented session line of the form
// "  <label>: <id>" where id falls back to "-" when empty. The
// leading spaces visually nest the line under its summary row even
// after tabwriter aligns the table above.
func formatSession(label, id string) string {
	if id == "" {
		id = "-"
	}
	return fmt.Sprintf("  %s: %s", label, id)
}
