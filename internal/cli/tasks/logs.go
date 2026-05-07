package tasks

import (
	"context"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/store/tasks"
)

// LogsOptions configures `j tasks logs`. Same shape as ReadOptions
// but the renderer is a Tailer (no bat/cat fallback): the leaf
// always execs `tail -f <agent.log>`.
type LogsOptions struct {
	TaskID string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	UI     UI
	Tailer Tailer
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
	if o.Tailer == nil {
		o.Tailer = defaultTailer
	}
	return o
}

// RunLogs implements `j tasks logs`. Resolves <cwd>/.j/tasks/<id>/
// agent.log via resolveTaskFile and hands the absolute path to the
// injected Tailer. Missing log -> "J: agent.log not found for task
// <id>" + exit 0 with no subprocess (matches the read leaves).
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
	return opts.Tailer(
		ctx, path, opts.Stdin, opts.Stdout, opts.Stderr,
	)
}

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail the resolved task's agent.log",
		Long: "Resolves a task (via --from-task or the shared " +
			"picker) and execs `tail -f <agent.log>` against " +
			"<cwd>/.j/tasks/<id>/agent.log. An unknown id prints " +
			"`J: no task` and a missing log prints `J: agent.log " +
			"not found for task <id>`. Both short-circuit exits 0 " +
			"with no subprocess. Read-only.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return RunLogs(cmd.Context(), LogsOptions{
				TaskID: viper.GetString("tasks.logs.from_task"),
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().String("from-task", "",
		"Tail the named task's agent.log (no picker)")
	_ = viper.BindPFlag("tasks.logs.from_task",
		cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.logs.from_task", "TASKS_LOGS_FROM_TASK")
	return cmd
}
