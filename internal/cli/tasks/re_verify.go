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
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

// ReVerifyUI is the slice of picker methods RunReVerify drives.
type ReVerifyUI interface {
	PickTask(ctx context.Context, ts []tasks.Task) (string, bool, error)
	ConfirmStatusOverride(
		ctx context.Context, cmd, taskID, status string,
	) (bool, error)
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
	if !tasks.IsLegal(task.Status, tasks.EventVerifyRestart) {
		return fmt.Errorf("cannot re-verify task in status %q", task.Status)
	}
	proceed, err := resolver.ConfirmStatusOverride(
		ctx, opts.UI, false, "re-verify", task, resolver.VerifyAllowed)
	if err != nil {
		return err
	}
	if !proceed {
		return nil
	}

	if task.VerifyResumeSession != "" {
		task.VerifyResumeSession = ""
		tasks.PersistWarn(opts.Stderr, task)
	}

	taskDir, err := tasks.EnsureDir(task.ID)
	if err != nil {
		return fmt.Errorf("ensure task dir: %w", err)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)

	interactive := resolver.Interactive(opts.Interactive)

	args := []string{
		cmdTasks, cmdOrchestrate,
		flagID, task.ID,
		flagPhaseVerifyOnly,
		"--interactive=" + strconv.FormatBool(interactive),
	}

	if err := takeoverIfHeld(ctx, opts.Stderr, task.ID); err != nil {
		return err
	}
	return launchOrchestrator(ctx, launchOptions{
		taskID:       task.ID,
		jBinary:      opts.JBinary,
		args:         args,
		agentLogPath: agentLogPath,
		interactive:  interactive,
		stdout:       opts.Stdout,
		stderr:       opts.Stderr,
	})
}

func resolveReVerifyTaskID(
	ctx context.Context, opts ReVerifyOptions,
) (string, bool, error) {
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

func pickReVerifyFromStore(
	ctx context.Context, s *tasks.Store, opts ReVerifyOptions,
) (string, bool, error) {
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
	agents := defaultAgents()
	cmd := &cobra.Command{
		Use: "re-verify",
		Short: "Re-verify an existing task: run the verifier inline " +
			"(--interactive) or detached",
		Long: "Resolves a task (via --from-task or the shared picker) " +
			"and either re-execs `j tasks orchestrate --phase=verify-only` " +
			"inline (with --interactive=true so the TUI can render in the " +
			"parent's terminal) or forks it as a detached child. Tasks in " +
			"work-done / failed / help skip the status-override prompt; " +
			"any other status renders a yes/no confirm before the " +
			"orchestrator runs.",
		PersistentPreRunE: preflight.PreRunE,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return preflight.EnsureAgentSelections(
				cmd.Context(),
				preflight.AgentCheckOptions{
					Stdin:  cmd.InOrStdin(),
					Stdout: cmd.OutOrStdout(),
					Stderr: cmd.ErrOrStderr(),
					Agents: agents,
				})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			interactive := explicitBoolPtr(cmd, flagKeyInteractive,
				"tasks.reverify.interactive",
				"TASKS_REVERIFY_INTERACTIVE")
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
	cmd.Flags().String(flagKeyFromTask, "",
		"Existing task id to re-verify (empty triggers the picker)")
	cmd.Flags().Bool(flagKeyInteractive, false,
		"Run verifier in interactive (TUI) mode")
	bindFlagEnv(cmd,
		flagEnvBinding{
			"tasks.reverify.from_task", flagKeyFromTask,
			"TASKS_REVERIFY_FROM_TASK",
		},
		flagEnvBinding{
			"tasks.reverify.interactive", flagKeyInteractive,
			"TASKS_REVERIFY_INTERACTIVE",
		},
	)
	return cmd
}
