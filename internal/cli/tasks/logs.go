package tasks

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/store/tasks"
)

// LogsOptions configures `j tasks logs`. Same shape as ShowOptions:
// Stdin/Stdout/Stderr default to the process streams; UI defaults to
// the huh-backed picker; Viewer defaults to defaultViewer (bat -> cat
// -> io.Copy). Tests pass scripted fakes for both UI and Viewer.
type LogsOptions struct {
	TaskID string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	UI     UI
	Viewer Viewer
}

func (o LogsOptions) withDefaults() LogsOptions {
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

// RunLogs implements `j tasks logs`. Resolves <cwd>/.j/tasks/<id>/
// agent.log via resolveTaskFile and hands the absolute path to the
// injected Viewer (bat -> cat -> io.Copy). Missing log -> "J:
// agent.log not found for task <id>" + exit 0 with no subprocess
// (matches the show leaves).
func RunLogs(ctx context.Context, opts LogsOptions) error {
	opts = opts.withDefaults()
	path, ok, err := resolveTaskFile(ctx, fileResolveOptions{
		TaskID: opts.TaskID,
		UI:     opts.UI,
		Stdin:  opts.Stdin,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	}, tasks.AgentLogFileName)
	if err != nil || !ok {
		return err
	}
	return opts.Viewer(
		ctx, path, opts.Stdin, opts.Stdout, opts.Stderr,
	)
}

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Render the resolved task's agent.log",
		Long: "Renders <cwd>/.j/tasks/<id>/agent.log via bat (when " +
			"installed and stdout is a TTY) or cat. Resolves the " +
			"task via --from-task or the shared picker; an unknown " +
			"id prints `J: no task` and a missing file prints " +
			"`J: agent.log not found for task <id>`. Both short-" +
			"circuit exits 0 with no subprocess. Read-only.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunLogs(cmd.Context(), LogsOptions{
				TaskID: viper.GetString("tasks.logs.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String(flagKeyFromTask, "",
		"Render the named task's agent.log (no picker)")
	_ = viper.BindPFlag("tasks.logs.from_task",
		cmd.Flags().Lookup(flagKeyFromTask))
	_ = viper.BindEnv("tasks.logs.from_task", "TASKS_LOGS_FROM_TASK")
	return cmd
}
