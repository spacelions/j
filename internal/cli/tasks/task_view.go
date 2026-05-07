package tasks

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/store/tasks"
)

// TaskViewOptions configures `j tasks task` — the bat/cat-rendered
// dump of <cwd>/.j/tasks/<id>/task.toml. Same shape as ReadOptions;
// the leaf is named TaskView to avoid colliding with the existing
// tasks.Task type.
type TaskViewOptions struct {
	TaskID string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	UI     UI
	Viewer Viewer
}

func (o TaskViewOptions) withDefaults() TaskViewOptions {
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
	if o.Viewer == nil {
		o.Viewer = defaultViewer
	}
	return o
}

// RunTaskView implements `j tasks task`. Resolves <cwd>/.j/tasks/
// <id>/task.toml via resolveTaskFile and forwards the absolute
// path to the injected Viewer.
func RunTaskView(ctx context.Context, opts TaskViewOptions) error {
	opts = opts.withDefaults()
	path, ok, err := resolveTaskFile(ctx, fileResolveOptions{
		TaskID: opts.TaskID,
		UI:     opts.UI,
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	}, tasks.TaskFileName)
	if err != nil || !ok {
		return err
	}
	return opts.Viewer(
		ctx, path, opts.Stdin, opts.Stdout, opts.Stderr,
	)
}

func newTaskViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Render the resolved task's task.toml",
		Long: "Resolves a task (via --from-task or the shared " +
			"picker) and renders <cwd>/.j/tasks/<id>/task.toml " +
			"via bat (when installed and stdout is a TTY) or cat. " +
			"An unknown id prints `J: no task` and a missing file " +
			"prints `J: task.toml not found for task <id>`. Both " +
			"short-circuit exits 0 with no subprocess. Read-only.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunTaskView(cmd.Context(), TaskViewOptions{
				TaskID: viper.GetString("tasks.task.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("from-task", "",
		"Render the named task's task.toml (no picker)")
	_ = viper.BindPFlag("tasks.task.from_task",
		cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.task.from_task", "TASKS_TASK_FROM_TASK")
	return cmd
}
