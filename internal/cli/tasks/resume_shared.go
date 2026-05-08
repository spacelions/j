package tasks

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store/tasks"
)

// resumeOptions is the common option set for all resume-* commands.
type resumeOptions struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	UI      UI
	JBinary string
}

func (o resumeOptions) withDefaults() resumeOptions {
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

// resumePhaseConfig captures what differs between resume-plan,
// resume-work, and resume-verify.
type resumePhaseConfig struct {
	emptyMsg        string
	resumeEvent     tasks.Event
	errorVerb       string
	hasSession      func(tasks.Task) bool
	orchestrateArgs func(taskID string) []string
}

// runResumePhase is the shared implementation for resume-plan,
// resume-work, and resume-verify.
func runResumePhase(
	ctx context.Context, opts resumeOptions, cfg resumePhaseConfig,
) (err error) {
	defer func() { err = resolver.CleanAbort(err) }()
	opts = opts.withDefaults()

	taskID, ok, err := pickResumeTaskID(ctx, opts, cfg)
	if err != nil || !ok {
		return err
	}
	t, err := resolver.TaskByID(taskID)
	if err != nil {
		return err
	}
	if !tasks.IsLegal(t.Status, cfg.resumeEvent) {
		return fmt.Errorf("cannot %s task in status %q",
			cfg.errorVerb, t.Status)
	}
	if _, err := tasks.EnsureDir(taskID); err != nil {
		return err
	}
	return runInlineOrchestrator(ctx, opts.JBinary, cfg.orchestrateArgs(taskID))
}

func pickResumeTaskID(
	ctx context.Context, opts resumeOptions, cfg resumePhaseConfig,
) (string, bool, error) {
	s, err := tasks.OpenDefault()
	if err != nil {
		return "", false, err
	}
	rows, err := s.ListTasks()
	_ = s.Close()
	if err != nil {
		return "", false, err
	}
	filtered := filterTasksBySession(rows, cfg.hasSession)
	if len(filtered) == 0 {
		uitheme.NormalFprintln(opts.Stdout, cfg.emptyMsg)
		return "", false, nil
	}
	tasks.SortTasks(filtered)
	return opts.UI.PickTask(ctx, filtered)
}

func filterTasksBySession(
	rows []tasks.Task, hasSession func(tasks.Task) bool,
) []tasks.Task {
	out := make([]tasks.Task, 0, len(rows))
	for _, t := range rows {
		if hasSession(t) {
			out = append(out, t)
		}
	}
	return out
}
