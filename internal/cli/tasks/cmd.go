// Package tasks implements the `j tasks` subcommand. It reads the
// per-project task log written by `j plan` and `j work` (the bbolt DB
// at `<cwd>/.j/tasks`) and prints a stable, human-readable list to
// stdout. No mutations are performed: editing, deleting, and resuming
// tasks are intentionally out of scope.
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
		Short: "List planning/work tasks recorded in <cwd>/.j/tasks",
		Long: "Reads the per-project task log written by `j plan` and " +
			"`j work` and prints a stable list to stdout. Active tasks " +
			"(planning, working, verifying, help) appear first; " +
			"completed tasks follow, sorted by done_at descending. " +
			"The RESUME column is the Cursor agent chat session id from " +
			"`cursor-agent create-chat` for that run; resume later with " +
			"`cursor-agent --resume <id>` (same id shown here). Shows - when " +
			"unset or not applicable.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listTasks(cmd.OutOrStdout())
		},
	}
}

// listTasks resolves the default tasks DB path, opens it, decodes
// every Task, sorts them via store.SortTasks, and writes a tab-aligned
// table to stdout. Missing DB or empty bucket prints emptyMessage.
func listTasks(stdout io.Writer) error {
	path, err := store.DefaultTasksPath()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			fmt.Fprintln(stdout, emptyMessage)
			return nil
		}
		return statErr
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

// writeTasks emits the header row plus one row per task to a
// tabwriter so the columns line up regardless of ID/summary length.
// The RESUME column is Task.ResumeCursor: the Cursor chat id for
// `cursor-agent --resume`, or "-" when empty. Time columns are omitted;
// callers that want timestamps can read the raw JSON via bbolt.
func writeTasks(out io.Writer, tasks []store.Task) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tSTATUS\tTOOL\tMODEL\tRESUME\tSUMMARY"); err != nil {
		return err
	}
	for _, t := range tasks {
		resume := formatResumeCursor(t.ResumeCursor)
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Status, t.InvokedTool, t.InvokedModel, resume, t.Summary); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// formatResumeCursor prints the Cursor session id for --resume, or "-"
// when there is no id to show.
func formatResumeCursor(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
