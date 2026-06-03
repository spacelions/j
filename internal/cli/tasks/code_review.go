package tasks

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

// CodeReviewOptions configures RunCodeReview. The detached parent
// process drives the parent flow (picker, validation) and either
// re-execs itself with hidden --run-round flags or runs the child
// loop inline when --interactive is set.
type CodeReviewOptions struct {
	// FromTask, when set, skips the picker and targets exactly the
	// named task. Empty falls back to the task picker filtered to
	// rows with a non-empty PullRequestURL.
	FromTask string
	// Interactive runs the child loop foreground (no detach, no
	// fork dialog). The agent backend's TUI gets the parent's
	// terminal so the planner can render interactively.
	Interactive bool

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	UI      UI
	Agents  []codingagents.Agent
	JBinary string
}

// codeReviewPickAdapter bridges the local UI surface to the
// resolver's CodeReviewPickUI without leaking the cli's broader UI
// interface into resolver.
type codeReviewPickAdapter struct{ UI }

func (a codeReviewPickAdapter) PickTask(
	ctx context.Context, rows []tasks.Task,
) (string, bool, error) {
	return a.UI.PickTask(ctx, rows)
}

// RunCodeReview is the parent body of `j tasks code-review`. It
// validates the picked task and either runs the child loop in the
// foreground (Interactive) or spawns the hidden child detached.
func RunCodeReview(
	ctx context.Context, opts CodeReviewOptions,
) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()
	if len(opts.Agents) == 0 {
		return errors.New("code-review: no coding agents configured")
	}
	taskID, ok, err := resolver.ResolveCodeReviewTaskID(
		ctx, codeReviewPickAdapter{opts.UI}, opts.Stderr, opts.FromTask)
	if err != nil || !ok {
		return err
	}
	row, err := resolver.TaskByID(taskID)
	if err != nil {
		uitheme.DangerousOutput(opts.Stderr, "J: %v", err)
		return err
	}
	if err := resolver.GuardCodeReviewTask(opts.Stderr, row); err != nil {
		return err
	}
	return launchCodeReviewChild(ctx, opts, taskID)
}

func launchCodeReviewChild(
	ctx context.Context, opts CodeReviewOptions, taskID string,
) error {
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		return err
	}
	args := []string{
		cmdTasks, cmdCodeReview,
		flagRunRound,
		flagCodeReviewFromTask, taskID,
	}
	if opts.Interactive {
		args = append(args, flagInteractiveTrue)
		return runInlineOrchestrator(ctx, opts.JBinary, args)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)
	pid, err := spawnDetachedOrchestrator(
		ctx, opts.JBinary, agentLogPath, args)
	if err != nil {
		return err
	}
	uitheme.NormalForkDialog(
		opts.Stdout, "task "+taskID+" code-review", pid, agentLogPath)
	return nil
}

func (o CodeReviewOptions) withDefaults() CodeReviewOptions {
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
