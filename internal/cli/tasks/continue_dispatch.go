package tasks

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/agents/worker"
)

// resumePlanInlineOrchestrator re-execs `j tasks orchestrate inline so
// the planner resumes its session in the foreground.
func resumePlanInlineOrchestrator(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	if _, err := resolver.StartTargetFromExistingTask(ctx, t.ID); err != nil {
		return err
	}
	if _, err := tasks.EnsureDir(t.ID); err != nil {
		return fmt.Errorf("J: ensure task dir: %w", err)
	}
	approval, _ := store.LoadPlanRequiresApproval()
	return runInlineOrchestrator(ctx, opts.JBinary, []string{
		"tasks", "orchestrate",
		"--id", t.ID,
		"--plan-requires-approval=" + strconv.FormatBool(approval),
		"--interactive=true",
	})
}

// resumeWorkInlineOrchestrator re-execs `j tasks orchestrate
// --phase=from-work --interactive=true` inline so the worker resumes
// its session in the foreground.
func resumeWorkInlineOrchestrator(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	if _, err := tasks.EnsureDir(t.ID); err != nil {
		return fmt.Errorf("J: ensure task dir: %w", err)
	}
	return runInlineOrchestrator(ctx, opts.JBinary, []string{
		"tasks", "orchestrate",
		"--id", t.ID,
		"--phase=from-work",
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
func dispatchHelp(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	switch latestPhase(t) {
	case "verify":
		return resumeVerifyingInline(ctx, opts, t.ID)
	case "work":
		return resumeWorkInlineOrchestrator(ctx, opts, t)
	case "plan":
		return resumePlanInlineOrchestrator(ctx, opts, t)
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
// stored worker bucket, then calls worker.Execute in-process.
func runPlanDoneWork(ctx context.Context, opts ContinueOptions, t tasks.Task) error {
	tool, model := resolver.ResolveToolModel(opts.Tool, opts.Model, store.BucketWorker, opts.Stderr)
	interactive := resolver.Interactive(nil, opts.Stderr, store.BucketWorker, opts.Interactive)
	return worker.Execute(ctx, worker.ExecuteOptions{
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
