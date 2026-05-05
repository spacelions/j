package tasks

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/cli/verify"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/workflow/agents/worker"
)

// resumeWorkInlineOrchestrator re-execs `j tasks orchestrate
// --skip-planning=true --interactive=true` inline so the worker
// resumes its session in the foreground.
func resumeWorkInlineOrchestrator(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	if _, err := tasks.EnsureDir(t.ID); err != nil {
		return fmt.Errorf("J: ensure task dir: %w", err)
	}
	return runInlineOrchestrator(ctx, opts.JBinary, []string{
		"tasks", "orchestrate",
		"--id", t.ID,
		"--skip-planning=true",
		"--interactive=true",
	})
}

// stampSpawnOnRow records BackgroundPID + AgentLogPath on the
// existing task row after a detached orchestrator spawn. Best-effort
// — any read / write error surfaces as a single warning on stderr.
// The detached child is already running, so we never roll back.
func stampSpawnOnRow(stderr io.Writer, taskID, agentLogPath string, pid int) {
	s, err := tasks.OpenDefault()
	if err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks dir: %v", err)
		return
	}
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(taskID)
	if err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks get %q: %v", taskID, err)
		return
	}
	row.AgentLogPath = agentLogPath
	row.BackgroundPID = pid
	if err := s.PutTask(row); err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks put: %v", err)
	}
}

// dispatchHelp picks a resume target for a `help` task. The latest
// completed phase wins — verify > work > plan when a phase end
// timestamp is present — because that is the phase that produced the
// failure mode the user is recovering from. When no phase timestamps
// are set we fall back to the resume cursor that is non-empty in the
// same precedence so a plan-time crash that never wrote PlanEndAt is
// still resumable. With no usable signal the dispatch errors instead
// of silently skipping.
//
// Plan-phase help is now a detached re-plan spawn (same as
// StatusPlanning) so the user can review the updated plan before work
// restarts.
func dispatchHelp(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	switch latestPhase(t) {
	case "verify":
		return verify.RunResume(ctx, verify.ResumeOptions{
			TaskID: t.ID,
			Stdin:  opts.Stdin,
			Stdout: opts.Stdout,
			Stderr: opts.Stderr,
			Agents: opts.Agents,
		})
	case "work":
		return resumeWorkInlineOrchestrator(ctx, opts, t)
	case "plan":
		if _, err := resolver.StartTargetFromExistingTask(ctx, t.ID); err != nil {
			return err
		}
		if _, err := tasks.EnsureDir(t.ID); err != nil {
			return fmt.Errorf("J: ensure task dir: %w", err)
		}
		return runInlineOrchestrator(ctx, opts.JBinary, []string{
			"tasks", "orchestrate",
			"--id", t.ID,
			"--plan-requires-approval=true",
			"--interactive=true",
		})
	}
	return fmt.Errorf("J: task %s in `help` has no resumable phase signal", t.ID)
}

// latestPhase returns "verify", "work", "plan", or "" depending on
// which phase has the freshest end timestamp (or, if none, which
// resume cursor is non-empty). Pulled out of dispatchHelp so the
// precedence is unit-testable in isolation.
func latestPhase(t tasks.Task) string {
	if v := latestEndAt(t); v != "" {
		return v
	}
	switch {
	case t.VerifyResumeSession != "":
		return "verify"
	case t.WorkResumeSession != "":
		return "work"
	case t.PlanResumeSession != "":
		return "plan"
	}
	return ""
}

// latestEndAt picks the phase whose EndAt timestamp is the most
// recent. Returns "" when every EndAt is zero.
func latestEndAt(t tasks.Task) string {
	pairs := []struct {
		name string
		t    time.Time
	}{
		{"verify", t.VerifyEndAt},
		{"work", t.WorkEndAt},
		{"plan", t.PlanEndAt},
	}
	var best string
	var bestT time.Time
	for _, p := range pairs {
		if p.t.IsZero() {
			continue
		}
		if best == "" || p.t.After(bestT) {
			best = p.name
			bestT = p.t
		}
	}
	return best
}

// runPlanDoneWork resolves the tool/model from explicit flags and the
// stored worker bucket, then calls worker.Run in-process.
func runPlanDoneWork(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	tool, model := resolver.ResolveToolModel(opts.Tool, opts.Model, store.BucketWorker, opts.Stderr)
	interactive := resolver.Interactive(nil, opts.Stderr, store.BucketWorker, opts.Interactive)
	return worker.Run(ctx, worker.Options{
		TaskID:      t.ID,
		Yes:         true,
		Interactive: interactive,
		Tool:        tool,
		Model:       model,
		Stdin:       opts.Stdin,
		Stdout:      opts.Stdout,
		Stderr:      opts.Stderr,
		Agents:      opts.Agents,
	})
}
