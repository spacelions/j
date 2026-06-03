package tasks

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/codereview"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/github"
	"github.com/spacelions/j/internal/util/agentlog"
	"github.com/spacelions/j/internal/util/run"
)

// CodeReviewChildOptions configures RunCodeReviewChild. The
// exported fields mirror the hidden child flags; cli tests inject
// scripted fetchers via Fetcher.
type CodeReviewChildOptions struct {
	TaskID      string
	Interactive bool
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Agents      []codingagents.Agent
	// Fetcher, when non-nil, replaces the real github.Client. The
	// cli wires the production fetcher via github.NewClient; tests
	// inject a scripted fetcher to avoid network IO.
	Fetcher resolver.CodeReviewFetcher
}

// RunCodeReviewChild is the body of the hidden
// `j tasks code-review --run-round` cobra invocation. It owns the
// long-running sequence (acquire flock, fetch, allocate round,
// write review.toml, run planner, validate) end to end.
func RunCodeReviewChild(
	ctx context.Context, opts CodeReviewChildOptions,
) error {
	opts = opts.withDefaults()
	if opts.TaskID == "" {
		return errors.New("code-review: --from-task required")
	}
	if len(opts.Agents) == 0 {
		return errors.New("code-review: no coding agents configured")
	}
	ctx = tasks.WithPhase(ctx, "code-review")
	lock, err := acquireCodeReviewLock(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	row, err := resolver.TaskByID(opts.TaskID)
	if err != nil {
		return err
	}
	if err := resolver.GuardCodeReviewTask(opts.Stderr, row); err != nil {
		return err
	}
	return executeCodeReviewRound(ctx, opts, row)
}

func acquireCodeReviewLock(
	ctx context.Context, opts CodeReviewChildOptions,
) (*tasks.Lock, error) {
	lock, err := tasks.AcquireLock(ctx, opts.TaskID)
	if err == nil {
		return lock, nil
	}
	var locked *tasks.LockedError
	if errors.As(err, &locked) {
		uitheme.DangerousOutput(opts.Stderr,
			"J: %s", contentionMessage(opts.TaskID, locked.Holder))
	}
	return nil, err
}

// executeCodeReviewRound runs the post-lock sequence: parse PR URL,
// fetch feedback, allocate round, write review.toml, run planner,
// wait for the agent grandchild (when headless), and validate.
func executeCodeReviewRound(
	ctx context.Context, opts CodeReviewChildOptions, row tasks.Task,
) error {
	ref, err := github.ParseURL(row.PullRequestURL)
	if err != nil {
		uitheme.DangerousOutput(opts.Stderr, "J: %v", err)
		return err
	}
	round, resumed, err := codereview.ResolveOrAllocate(opts.TaskID)
	if err != nil {
		return err
	}
	emitRoundMarker(opts.Stderr, opts.TaskID, round)
	fetcher := opts.Fetcher
	if fetcher == nil {
		fetcher = github.NewClient(github.ResolveToken())
	}
	originalIDs, err := resolver.FetchAndWriteReview(
		ctx, fetcher, round, ref, opts.Stderr)
	if err != nil {
		return err
	}
	if err := runCodeReviewPlanner(ctx, opts, round, resumed); err != nil {
		return err
	}
	return resolver.ValidateReviewRound(round, originalIDs, opts.Stderr)
}

func runCodeReviewPlanner(
	ctx context.Context, opts CodeReviewChildOptions,
	round codereview.Round, resumed bool,
) error {
	agent, model, err := resolver.ResolvePlannerAgent(
		ctx, opts.Agents, opts.Stderr)
	if err != nil {
		return err
	}
	taskDir := filepath.Dir(filepath.Dir(round.Dir))
	mustRead, _ := resolver.MustRead()
	req := codingagents.CodeReviewRequest{
		TaskDir:             taskDir,
		Model:               model,
		ReviewTOMLPath:      round.ReviewTOMLPath,
		RequirementsPath:    filepath.Join(taskDir, tasks.RequirementsFileName),
		PlanPath:            filepath.Join(taskDir, tasks.PlanFileName),
		RoundPlanOutputPath: round.PlanPath,
		ClarificationPath:   round.ClarificationPath,
		Interactive:         opts.Interactive,
		AgentLogPath:        filepath.Join(taskDir, tasks.AgentLogFileName),
		Resume:              resumed,
		MustRead:            mustRead,
	}
	pid, err := codingagents.RunCodeReview(ctx, agent, req)
	if err != nil {
		return err
	}
	return waitOrTerminate(ctx, pid)
}

// waitOrTerminate waits for pid to exit. If ctx is cancelled while
// the planner is still running, the planner is signalled (SIGTERM
// then SIGKILL after the grace) before this function returns. That
// matters because the per-task flock is released as soon as
// RunCodeReviewChild unwinds — without an explicit terminate, a
// later code-review invocation could acquire the lock and race the
// orphaned planner still writing review.toml or plan.md.
func waitOrTerminate(ctx context.Context, pid int) error {
	if pid == 0 {
		return nil
	}
	err := run.WaitForExit(ctx, pid)
	if err == nil {
		return nil
	}
	if !errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	_, _ = run.Terminate(context.Background(), pid, codeReviewTerminateGrace)
	return err
}

// codeReviewTerminateGrace mirrors the resume-* takeover grace so a
// cancelled planner has the same window to react to SIGTERM before
// SIGKILL escalates.
const codeReviewTerminateGrace = 2 * time.Second

func emitRoundMarker(w io.Writer, taskID string, round codereview.Round) {
	_ = agentlog.Emit(w, "code_review_round", map[string]any{
		"task":  taskID,
		"round": round.N,
		"dir":   round.Dir,
	})
}

func (o CodeReviewChildOptions) withDefaults() CodeReviewChildOptions {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	return o
}
