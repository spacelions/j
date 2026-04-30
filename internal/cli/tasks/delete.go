package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/store"
)

// noTaskMessage is the single line printed to stdout when the named
// task does not exist (bbolt bucket missing or key absent). Pinning
// it in a constant keeps the test assertion and the command output
// in lockstep.
const noTaskMessage = "J: no task"

// abortedMessage is the single line printed to stdout when the user
// declines the confirmation prompt. Same lockstep concern as
// noTaskMessage.
const abortedMessage = "delete aborted"

// DeleteOptions configures RunDelete. Stdin/Stdout/Stderr default to
// the process streams; UI defaults to the huh-backed implementation.
// Tests pass a scripted fake for UI to avoid touching stdin.
type DeleteOptions struct {
	// TaskID is the exact bbolt key (Task.ID, a 26-char ULID) of the
	// row to remove. Empty values are rejected with a wrapped error
	// before any IO; cobra's MarkFlagRequired covers the "no flag"
	// case ahead of RunDelete.
	TaskID string
	// Yes, when true, skips the confirm-delete prompt and proceeds
	// to the wipe path. Sourced from the --yes/-y flag and the
	// tasks.delete.yes viper key.
	Yes bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	UI UI
}

// withDefaults fills the nil Stdin/Stdout/Stderr with the process
// streams and instantiates a huh-backed UI when one was not
// supplied. The pattern matches initcmd.Options.withDefaults so
// every j subcommand defaults uniformly.
func (o DeleteOptions) withDefaults() DeleteOptions {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.UI == nil {
		o.UI = newHuhUI(o.Stdin, o.Stderr)
	}
	return o
}

// RunDelete executes `j tasks delete`. The state machine is:
//
//  1. Reject an empty TaskID (defense in depth; cobra normally
//     surfaces the missing-flag error first).
//  2. Open <cwd>/.j/tasks/list.db. GetTask wraps fs.ErrNotExist when
//     the bucket is missing or the key is absent; in that case
//     print "J: no task" and return nil (exit 0). Other errors
//     propagate wrapped.
//  3. When --yes is unset, render the confirm prompt; on decline
//     print "delete aborted" and return nil. UI implementations
//     map huh.ErrUserAborted to (false, nil) so a Ctrl-C is
//     indistinguishable from an explicit decline.
//  4. On confirm, DeleteTask removes the bbolt row.
//  5. RemoveTaskDir deletes <cwd>/.j/tasks/<id>/ recursively. The
//     bbolt file lives at <tasksDir>/list.db, a sibling of the
//     per-task directory, so RemoveTaskDir can run while the
//     store handle is still open.
//  6. On success, print "J: deleted <id>" and return nil.
//
// The store is closed via defer so every return path releases the
// bbolt file lock before the next `j tasks` invocation tries to
// re-acquire it.
func RunDelete(ctx context.Context, opts DeleteOptions) error {
	opts = opts.withDefaults()
	if opts.TaskID == "" {
		return errors.New("tasks delete: --id is required")
	}
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		return err
	}
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	task, err := s.GetTask(opts.TaskID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(opts.Stdout, noTaskMessage)
			return nil
		}
		return err
	}
	if !opts.Yes {
		ok, err := opts.UI.ConfirmDelete(ctx, task)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(opts.Stdout, abortedMessage)
			return nil
		}
	}
	if err := s.DeleteTask(opts.TaskID); err != nil {
		return fmt.Errorf("tasks delete: %w", err)
	}
	if err := store.RemoveTaskDir(opts.TaskID); err != nil {
		return fmt.Errorf("tasks delete: %w", err)
	}
	fmt.Fprintf(opts.Stdout, "J: deleted %s\n", opts.TaskID)
	return nil
}

// newDeleteCmd builds the `j tasks delete` cobra subcommand and its
// flag bindings. The parent command's PersistentPreRunE (preflight)
// is inherited automatically, so the missing-init prompt fires
// here too. viper.BindPFlag / viper.BindEnv only fail on programmer
// errors (nil flag, empty key) so the returned errors are
// intentionally discarded, matching the rest of the j CLI.
func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a task row and its on-disk directory",
		Long: "Removes a single task from <cwd>/.j/tasks/list.db AND the " +
			"matching on-disk directory <cwd>/.j/tasks/<id>/. The --id flag is " +
			"required and must match an existing task ID exactly. Without " +
			"--yes, a confirmation prompt is rendered (default Enter/`y` " +
			"accepts) so you can recognise the row from its summary before " +
			"committing. Unknown ids print `J: no task` and exit 0.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunDelete(cmd.Context(), DeleteOptions{
				TaskID: viper.GetString("tasks.delete.id"),
				Yes:    viper.GetBool("tasks.delete.yes"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("id", "", "Task ID to delete (required)")
	cmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt and delete immediately")
	_ = cmd.MarkFlagRequired("id")
	_ = viper.BindPFlag("tasks.delete.id", cmd.Flags().Lookup("id"))
	_ = viper.BindPFlag("tasks.delete.yes", cmd.Flags().Lookup("yes"))
	_ = viper.BindEnv("tasks.delete.id", "TASKS_DELETE_ID")
	_ = viper.BindEnv("tasks.delete.yes", "TASKS_DELETE_YES")
	return cmd
}
