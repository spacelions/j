package tasks

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spacelions/j/internal/cli/tasks/codereview"
	"github.com/spacelions/j/internal/cli/uitheme"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/tools/github"
	"github.com/spacelions/j/internal/util/agentlog"
	"github.com/spacelions/j/internal/util/run"
)

// CodeReviewChildOptions configures RunCodeReviewChild. The exported
// fields mirror the hidden child flags; cli tests inject scripted
// fetchers via Fetcher.
type CodeReviewChildOptions struct {
	TaskID      string
	Interactive bool
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Agents      []codingagents.Agent
	// Fetcher, when non-nil, replaces the real github.Client. The cli
	// wires the production fetcher via newGithubFetcher; tests inject
	// a scripted one to avoid network IO.
	Fetcher CodeReviewFetcher
}

// CodeReviewFetcher narrows the github.Client surface the child
// process uses. The production cli wires github.NewClient + FetchPR;
// tests inject a scripted fetcher that returns canned FetchResult
// values without an httptest.Server.
type CodeReviewFetcher interface {
	FetchPR(
		ctx context.Context, ref github.PRRef,
	) (github.FetchResult, error)
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
	if err := guardCodeReviewTask(opts.Stderr, row); err != nil {
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
		uitheme.DangerousDialogBox(opts.Stderr,
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
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
		return err
	}
	result, err := fetchPR(ctx, opts, ref)
	if err != nil {
		return err
	}
	round, err := resolveOrAllocateRound(opts.TaskID)
	if err != nil {
		return err
	}
	emitRoundMarker(opts.Stderr, opts.TaskID, round)
	originalIDs, err := writeFetchedReview(round, result)
	if err != nil {
		return err
	}
	if err := runCodeReviewPlanner(ctx, opts, round); err != nil {
		return err
	}
	return validateReviewFile(round.ReviewTOMLPath, originalIDs, opts.Stderr)
}

func fetchPR(
	ctx context.Context, opts CodeReviewChildOptions, ref github.PRRef,
) (github.FetchResult, error) {
	fetcher := opts.Fetcher
	if fetcher == nil {
		fetcher = github.NewClient(github.ResolveToken())
	}
	res, err := fetcher.FetchPR(ctx, ref)
	if err != nil {
		uitheme.DangerousDialogBox(opts.Stderr, "J: %v", err)
		return github.FetchResult{}, err
	}
	return res, nil
}

// writeFetchedReview serialises the fetched feedback into the round
// review.toml and returns the snapshot of source ids the validator
// expects to find after the planner runs.
func writeFetchedReview(
	round codeReviewRound, res github.FetchResult,
) (codereview.SourceIDSet, error) {
	file := codereview.ReviewFile{
		SchemaVersion: codereview.SchemaVersion,
		Provider:      "github",
		FetchedAt:     time.Now().UTC(),
		PR: codereview.PR{
			URL:    res.PR.URL,
			Owner:  res.PR.Owner,
			Repo:   res.PR.Repo,
			Number: res.PR.Number,
			State:  res.PR.State,
			Draft:  res.PR.Draft,
			Merged: res.PR.Merged,
		},
		Items: itemsFromFetch(res.Items),
	}
	if err := codereview.Save(round.ReviewTOMLPath, file); err != nil {
		return nil, err
	}
	return codereview.SnapshotSourceIDs(file), nil
}

func itemsFromFetch(in []github.Item) []codereview.Item {
	out := make([]codereview.Item, 0, len(in))
	for _, it := range in {
		out = append(out, codereview.Item{
			SourceID:   it.SourceID,
			Kind:       string(it.Kind),
			ThreadID:   it.ThreadID,
			Author:     it.Author,
			Body:       it.Body,
			Path:       it.Path,
			Line:       it.Line,
			IsOutdated: it.IsOutdated,
			HasJReply:  it.HasJReply,
		})
	}
	return out
}

func runCodeReviewPlanner(
	ctx context.Context, opts CodeReviewChildOptions, round codeReviewRound,
) error {
	agent, model, err := resolvePlannerAgent(ctx, opts)
	if err != nil {
		return err
	}
	taskDir := filepath.Dir(filepath.Dir(round.Dir))
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
	}
	pid, err := codingagents.RunCodeReview(ctx, agent, req)
	if err != nil {
		return err
	}
	if pid == 0 {
		return nil
	}
	return run.WaitForExit(ctx, pid)
}

// resolvePlannerAgent opens the project settings store, reads the
// planner bucket's tool/model pair, and returns the matching agent.
// The store is closed before returning so the bbolt file lock is not
// held across the long planner round-trip.
func resolvePlannerAgent(
	ctx context.Context, opts CodeReviewChildOptions,
) (codingagents.Agent, string, error) {
	s, ok := store.OpenSettings(opts.Stderr)
	if !ok {
		return nil, "", resolver.ErrNoStoredSelection
	}
	defer func() { _ = s.Close() }()
	return resolver.AgentFromStore(ctx, s, store.BucketPlanner, opts.Agents)
}

func validateReviewFile(
	path string, ids codereview.SourceIDSet, stderr io.Writer,
) error {
	file, err := codereview.Load(path)
	if err != nil {
		uitheme.DangerousDialogBox(stderr, "J: %v", err)
		return err
	}
	if err := codereview.ValidatePost(file, ids); err != nil {
		uitheme.DangerousDialogBox(stderr, "J: %v", err)
		return err
	}
	return nil
}

func emitRoundMarker(w io.Writer, taskID string, round codeReviewRound) {
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
