package tasks

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// resumePlanInlineOrchestrator re-execs `j tasks orchestrate inline so
// the planner resumes its session in the foreground.
func resumePlanInlineOrchestrator(
	ctx context.Context, opts ContinueOptions, t tasks.Task,
) error {
	if _, err := resolver.StartTargetFromExistingTask(ctx, t.ID); err != nil {
		return err
	}
	if _, err := tasks.EnsureDir(t.ID); err != nil {
		return fmt.Errorf("ensure task dir: %w", err)
	}
	approval, _ := store.LoadPlanRequiresApproval()
	return runInlineOrchestrator(ctx, opts.JBinary, []string{
		cmdTasks, cmdOrchestrate,
		flagID, t.ID,
		"--plan-requires-approval=" + strconv.FormatBool(approval),
		flagInteractiveTrue,
	})
}

// resumeWorkInlineOrchestrator re-execs `j tasks orchestrate
// --phase=from-work --interactive=true` inline so the worker resumes
// its session in the foreground.
func resumeWorkInlineOrchestrator(
	ctx context.Context, opts ContinueOptions, t tasks.Task,
) error {
	if _, err := tasks.EnsureDir(t.ID); err != nil {
		return fmt.Errorf("ensure task dir: %w", err)
	}
	return runInlineOrchestrator(ctx, opts.JBinary, []string{
		cmdTasks, cmdOrchestrate,
		flagID, t.ID,
		flagPhaseFromWork,
		flagInteractiveTrue,
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
func dispatchHelp(
	ctx context.Context, opts ContinueOptions, t tasks.Task,
) error {
	switch latestPhase(t) {
	case "verify":
		return resumeVerifyingInline(ctx, opts, t.ID)
	case "work":
		return resumeWorkInlineOrchestrator(ctx, opts, t)
	case cmdPlan:
		return resumePlanInlineOrchestrator(ctx, opts, t)
	}
	return fmt.Errorf("task %s in `help` has no resumable phase signal", t.ID)
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

// runPlanDoneWork dispatches a plan-done task through the orchestrator
// the same way `re-work` does so the worker → verifier chain runs to a
// terminal status. Inline when --interactive=true; detached otherwise.
// --tool / --model overrides forward into the orchestrate argv; the
// child resolves the worker bucket itself when the flags are absent.
func runPlanDoneWork(
	ctx context.Context, opts ContinueOptions, t tasks.Task,
) error {
	taskDir, err := tasks.EnsureDir(t.ID)
	if err != nil {
		return fmt.Errorf("ensure task dir: %w", err)
	}
	interactive := resolver.Interactive(opts.Interactive)
	args := []string{
		cmdTasks, cmdOrchestrate,
		flagID, t.ID,
		flagPhaseFromWork,
		"--interactive=" + strconv.FormatBool(interactive),
	}
	if opts.Tool != "" {
		args = append(args, "--tool="+opts.Tool)
	}
	if opts.Model != "" {
		args = append(args, "--model="+opts.Model)
	}
	if interactive {
		return runInlineOrchestrator(ctx, opts.JBinary, args)
	}
	agentLogPath := filepath.Join(taskDir, tasks.AgentLogFileName)
	pid, err := spawnDetachedOrchestrator(
		ctx, opts.JBinary, agentLogPath, args)
	if err != nil {
		return err
	}
	stampSpawnOnRow(opts.Stderr, t.ID, agentLogPath, pid)
	uitheme.NormalForkDialog(
		opts.Stdout, "task "+t.ID, pid, agentLogPath)
	return nil
}

// dispatchPlanApprove fires EventPlanApprove and falls through to work
// in the same call. Running `j tasks continue` on a
// plan-pending-approval row IS the approval signal.
func dispatchPlanApprove(
	ctx context.Context, opts ContinueOptions, t tasks.Task,
) error {
	prev := t.Status
	if _, err := tasks.ApplyAndPersistWarn(
		opts.Stderr, &t, tasks.EventPlanApprove); err != nil {
		return fmt.Errorf("cannot approve task in status %q", prev)
	}
	return runPlanDoneWork(ctx, opts, t)
}

// dispatchClarification picks the right Resume event via latestPhase,
// fires it, and launches the orchestrator inline with
// --interactive=true (overriding the background default).
func dispatchClarification(
	ctx context.Context, opts ContinueOptions, t tasks.Task,
) error {
	var ev tasks.Event
	switch latestPhase(t) {
	case "verify":
		ev = tasks.EventVerifyResume
	case "work":
		ev = tasks.EventWorkResume
	case cmdPlan:
		ev = tasks.EventPlanResume
	default:
		return fmt.Errorf(
			"task %s in %s has no resumable phase",
			t.ID, tasks.StatusNeedsClarification)
	}
	prev := t.Status
	if _, err := tasks.ApplyAndPersistWarn(
		opts.Stderr, &t, ev); err != nil {
		return fmt.Errorf(
			"cannot resume task in status %q (event %q)",
			prev, ev)
	}

	switch ev {
	case tasks.EventVerifyResume:
		return resumeVerifyingInline(ctx, opts, t.ID)
	case tasks.EventWorkResume:
		return resumeWorkInlineOrchestrator(ctx, opts, t)
	default:
		return resumePlanInlineOrchestrator(ctx, opts, t)
	}
}
