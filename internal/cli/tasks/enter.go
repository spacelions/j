package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/store"
)

// Spawner is the subshell launcher used by `j tasks enter`. It is
// defined as a concrete func type (not an exported interface) so the
// substitution surface stays inside the tasks package — tests pass a
// fake Spawner via EnterOptions, mirroring how the UI interface is
// swapped, without leaking a new abstraction.
type Spawner func(ctx context.Context, dir string, in io.Reader, out, errw io.Writer) error

// defaultSpawner is the production Spawner. It resolves $SHELL with
// a /bin/sh fallback, builds an exec.CommandContext rooted at the
// chosen task directory, wires the streams, and returns the wrapped
// exit error so cobra surfaces a non-zero shell exit code.
func defaultSpawner(ctx context.Context, dir string, in io.Reader, out, errw io.Writer) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.Dir = dir
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errw
	return cmd.Run()
}

// EnterOptions configures RunEnter. Stdin/Stdout/Stderr default to
// the process streams; UI / Spawner default to the huh-backed and
// $SHELL-backed implementations. Tests pass scripted fakes for both
// so no real subshell launches in CI.
type EnterOptions struct {
	// TaskID is the bbolt key (Task.ID) of the row to enter. Empty
	// triggers the picker fallback. Sourced from the --id flag and
	// the tasks.enter.id viper key.
	TaskID string
	// Print, when true, prints the resolved task directory to stdout
	// instead of spawning a subshell. Sourced from the --print flag
	// and the tasks.enter.print viper key.
	Print bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	UI      UI
	Spawner Spawner
}

// withDefaults fills the nil Stdin/Stdout/Stderr with the process
// streams and instantiates the huh-backed UI / defaultSpawner when
// not supplied. The pattern matches DiscardOptions.withDefaults so
// every j subcommand defaults uniformly.
func (o EnterOptions) withDefaults() EnterOptions {
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
	if o.Spawner == nil {
		o.Spawner = defaultSpawner
	}
	return o
}

// RunEnter executes `j tasks enter`. The state machine is:
//
//  1. Resolve the task id. With opts.TaskID set, GetTask is queried
//     directly; an absent row prints "J: no task" and returns nil.
//     With opts.TaskID empty, the bbolt store is opened, ListTasks
//     is consulted, and the picker UI selects a row. An empty list
//     (or missing list.db) prints "J: no tasks" and returns nil; a
//     user-abort in the picker returns ("", nil) silently.
//  2. The bbolt store is closed before the spawn so the file lock
//     is released ahead of any long-running subshell.
//  3. EnsureTaskDir guarantees the per-task directory exists on
//     disk so older rows minted before EnsureTaskDir was wired in
//     can still be entered.
//  4. With --print, the absolute path is written to opts.Stdout
//     terminated by a newline and RunEnter returns. Otherwise the
//     Spawner is invoked with the resolved dir; its error is
//     wrapped with "tasks enter: " so cobra surfaces a deterministic
//     prefix on a non-zero shell.
func RunEnter(ctx context.Context, opts EnterOptions) error {
	opts = opts.withDefaults()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		return err
	}
	if opts.TaskID == "" {
		if _, statErr := os.Stat(path); errors.Is(statErr, fs.ErrNotExist) {
			banner.Fprintln(opts.Stdout, emptyMessage)
			return nil
		}
	}
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	id, ok, err := resolveEnterID(ctx, s, opts)
	_ = s.Close()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	taskDir, err := store.EnsureTaskDir(id)
	if err != nil {
		return fmt.Errorf("tasks enter: %w", err)
	}
	if opts.Print {
		fmt.Fprintln(opts.Stdout, taskDir)
		return nil
	}
	if err := opts.Spawner(ctx, taskDir, opts.Stdin, opts.Stdout, opts.Stderr); err != nil {
		return fmt.Errorf("tasks enter: %w", err)
	}
	return nil
}

// resolveEnterID centralises the id resolution branches once a
// bbolt store handle is open. The bool return distinguishes "id
// was resolved" from "the command emitted a user-facing message
// and should exit 0": callers continue with the spawn / print path
// only when ok is true. The supplied store handle is reused (no
// second open) so the bbolt lock is acquired exactly once per
// invocation. The picker branch delegates to pickFromStore so the
// same selector body backs both `tasks enter` and `tasks discard`.
func resolveEnterID(ctx context.Context, s *store.Store, opts EnterOptions) (string, bool, error) {
	if opts.TaskID != "" {
		if _, err := s.GetTask(opts.TaskID); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				banner.Fprintln(opts.Stdout, noTaskMessage)
				return "", false, nil
			}
			return "", false, err
		}
		return opts.TaskID, true, nil
	}
	return pickFromStore(ctx, s, opts.UI, opts.Stdout)
}

// newEnterCmd builds the `j tasks enter` cobra subcommand and its
// flag bindings. The parent command's PersistentPreRunE (preflight)
// is inherited automatically so the missing-init prompt fires here
// too. viper.BindPFlag / viper.BindEnv only fail on programmer
// errors (nil flag, empty key) so the returned errors are
// intentionally discarded, matching the rest of the j CLI.
func newEnterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enter",
		Short: "Pick a task and jump into its directory",
		Long: "Picks a task from <cwd>/.j/tasks/list.db (via --id or a " +
			"huh selector) and either spawns an interactive subshell " +
			"rooted at the chosen task's directory or prints the " +
			"absolute path to stdout (--print). Unknown ids print " +
			"`J: no task` and exit 0; an empty store prints " +
			"`J: no tasks`. Ctrl-C / Esc inside the picker exits 0.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunEnter(cmd.Context(), EnterOptions{
				TaskID: viper.GetString("tasks.enter.id"),
				Print:  viper.GetBool("tasks.enter.print"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("id", "", "Task ID to enter (empty triggers the picker)")
	cmd.Flags().Bool("print", false, "Print the absolute task directory instead of spawning a subshell")
	_ = viper.BindPFlag("tasks.enter.id", cmd.Flags().Lookup("id"))
	_ = viper.BindPFlag("tasks.enter.print", cmd.Flags().Lookup("print"))
	_ = viper.BindEnv("tasks.enter.id", "TASKS_ENTER_ID")
	_ = viper.BindEnv("tasks.enter.print", "TASKS_ENTER_PRINT")
	return cmd
}
