package tasks

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// ReVerifyUI is the slice of picker methods RunReVerify drives.
type ReVerifyUI interface {
	PickTask(ctx context.Context, ts []tasks.Task) (string, bool, error)
	ConfirmStatusOverride(ctx context.Context, cmd, taskID, status string) (bool, error)
}

// ReVerifyOptions configures RunReVerify.
type ReVerifyOptions struct {
	FromTask    string
	Interactive *bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Agents []codingagents.Agent
	UI     ReVerifyUI

	// JBinary is the absolute path to the j binary re-executed.
	// Empty falls back to os.Executable.
	JBinary string
}

func (o ReVerifyOptions) withDefaults() ReVerifyOptions {
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

// RunReVerify implements `j tasks re-verify`. It resolves a task
// and re-execs `j tasks orchestrate --phase=verify-only` either
// inline (--interactive=true) or detached.
func RunReVerify(ctx context.Context, opts ReVerifyOptions) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()

	taskID, ok, err := resolveReVerifyTaskID(ctx, opts)
	if err != nil || !ok {
		return err
	}
	task, err := resolver.TaskByID(taskID)
	if err != nil {
		return err
	}
	proceed, err := resolver.ConfirmStatusOverride(ctx, opts.UI, false, "re-verify", task, resolver.VerifyAllowed)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	taskDir, err := tasks.EnsureDir(task.ID)
	if err != nil {
		return fmt.Errorf("J: ensure task dir: %w", err)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	interactive := resolver.Interactive(nil, opts.Stderr, store.BucketVerifier, opts.Interactive)

	args := []string{
		"tasks", "orchestrate",
		"--id", task.ID,
		"--phase=verify-only",
		"--interactive=" + strconv.FormatBool(interactive),
	}

	if interactive {
		stampSpawnOnRow(opts.Stderr, task.ID, "", 0)
		return runInlineOrchestrator(ctx, opts.JBinary, args)
	}

	pid, err := spawnDetachedOrchestrator(ctx, opts.JBinary, agentLogPath, args)
	if err != nil {
		return err
	}
	stampSpawnOnRow(opts.Stderr, task.ID, agentLogPath, pid)
	uitheme.NormalForkDialog(opts.Stdout, fmt.Sprintf("task %s", task.ID), pid, agentLogPath)
	return nil
}

func resolveReVerifyTaskID(ctx context.Context, opts ReVerifyOptions) (string, bool, error) {
	if opts.FromTask != "" {
		return opts.FromTask, true, nil
	}
	s, err := tasks.OpenDefault()
	if err != nil {
		return "", false, err
	}
	id, ok, err := pickReVerifyFromStore(ctx, s, opts)
	_ = s.Close()
	return id, ok, err
}

func pickReVerifyFromStore(ctx context.Context, s *tasks.Store, opts ReVerifyOptions) (string, bool, error) {
	rows, err := s.ListTasks()
	if err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		uitheme.NormalFprintln(opts.Stdout, emptyMessage)
		return "", false, nil
	}
	tasks.SortTasks(rows)
	return opts.UI.PickTask(ctx, rows)
}

// newReVerifyCmd builds the `j tasks re-verify` cobra subcommand.
func newReVerifyCmd() *cobra.Command {
	agents := []codingagents.Agent{cursor.New(), claude.New()}
	cmd := &cobra.Command{
		Use:   "re-verify",
		Short: "Re-verify an existing task: run the verifier inline (--interactive) or detached",
		Long: "Resolves a task (via --from-task or the shared picker) and either " +
			"re-execs `j tasks orchestrate --phase=verify-only` inline " +
			"(with --interactive=true so the TUI can render in the parent's terminal) " +
			"or forks it as a detached child. Tasks in work-done / verify-done / help " +
			"skip the status-override prompt; any other status renders a yes/no confirm " +
			"before the orchestrator runs.",
		PersistentPreRunE: preflight.PreRunE,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return preflight.EnsureAgentSelections(cmd.Context(), preflight.AgentCheckOptions{
				Stdin:  cmd.InOrStdin(),
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
				Agents: agents,
			})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			var interactive *bool
			if cmd.Flags().Changed("interactive") || envSet("TASKS_REVERIFY_INTERACTIVE") {
				v := viper.GetBool("tasks.reverify.interactive")
				interactive = &v
			}
			return RunReVerify(cmd.Context(), ReVerifyOptions{
				FromTask:    viper.GetString("tasks.reverify.from_task"),
				Interactive: interactive,
				Stdin:       cmd.InOrStdin(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Agents:      agents,
			})
		},
	}
	cmd.Flags().String("from-task", "", "Existing task id to re-verify (empty triggers the picker)")
	cmd.Flags().Bool("interactive", false, "Run verifier in interactive (TUI) mode")
	_ = viper.BindPFlag("tasks.reverify.from_task", cmd.Flags().Lookup("from-task"))
	_ = viper.BindEnv("tasks.reverify.from_task", "TASKS_REVERIFY_FROM_TASK")
	_ = viper.BindPFlag("tasks.reverify.interactive", cmd.Flags().Lookup("interactive"))
	_ = viper.BindEnv("tasks.reverify.interactive", "TASKS_REVERIFY_INTERACTIVE")
	return cmd
}
