package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

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

// disallowedStatuses are the lifecycle states the parent rejects
// before spawning the child. Mid-flight tasks share their flock with
// the orchestrator and the code-review command must not race them.
var disallowedStatuses = map[tasks.TaskStatus]bool{
	tasks.StatusPlanning:  true,
	tasks.StatusWorking:   true,
	tasks.StatusVerifying: true,
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
	taskID, ok, err := resolveCodeReviewTaskID(ctx, opts)
	if err != nil || !ok {
		return err
	}
	row, err := resolver.TaskByID(taskID)
	if err != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
		return err
	}
	if err := guardCodeReviewTask(opts.Stderr, row); err != nil {
		return err
	}
	return launchCodeReviewChild(ctx, opts, taskID)
}

// guardCodeReviewTask is the parent's pre-flight check. It rejects
// tasks with no PR URL and tasks in lifecycle states that hold the
// per-task flock for the orchestrator.
func guardCodeReviewTask(stderr io.Writer, row tasks.Task) error {
	if row.PullRequestURL == "" {
		uitheme.DangerousDialogBox(stderr,
			"J: task %s has no PullRequestURL; "+
				"set it via the worker turn first", row.ID)
		return fmt.Errorf("code-review: task %s has no PR URL", row.ID)
	}
	if disallowedStatuses[row.Status] {
		uitheme.DangerousDialogBox(stderr,
			"J: task %s is %s; wait for the orchestrator "+
				"to finish before reviewing", row.ID, row.Status)
		return fmt.Errorf(
			"code-review: task %s status %q forbids review",
			row.ID, row.Status)
	}
	return nil
}

func resolveCodeReviewTaskID(
	ctx context.Context, opts CodeReviewOptions,
) (string, bool, error) {
	if opts.FromTask != "" {
		return opts.FromTask, true, nil
	}
	rows, err := listTasksWithPR()
	if err != nil {
		return "", false, err
	}
	if len(rows) == 0 {
		uitheme.DangerousDialogBox(opts.Stderr,
			"J: no tasks with a stored pull request URL; "+
				"run `j tasks start` and let the worker open a PR first")
		return "", false, errors.New("code-review: no eligible tasks")
	}
	tasks.SortTasks(rows)
	id, ok, err := opts.UI.PickTask(ctx, rows)
	if err != nil {
		return "", false, err
	}
	return id, ok, nil
}

func listTasksWithPR() ([]tasks.Task, error) {
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	all, err := s.ListTasks()
	if err != nil {
		return nil, err
	}
	out := make([]tasks.Task, 0, len(all))
	for _, t := range all {
		if t.PullRequestURL == "" {
			continue
		}
		out = append(out, t)
	}
	return out, nil
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

// parseRoundFlag converts the optional `--round <n>` flag value into
// a positive integer. Empty falls back to 0 which means "let the
// allocator decide" (the typical resume path).
func parseRoundFlag(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
