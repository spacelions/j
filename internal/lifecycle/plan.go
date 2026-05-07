// Package lifecycle owns the per-phase begin/end task-log writes
// and the planner → worker → verifier orchestration that drives
// `j tasks` end to end. The per-phase helpers (NewPlanTask,
// BeginWorkReuse, BeginVerifyResume, ...) live in this package's
// root; the SequentialAgent / launcher wiring lives in
// internal/lifecycle/orchestrator/ to avoid an import cycle with
// internal/agents/{planner,worker,verifier}, which in turn call back
// into the per-phase helpers here.
package lifecycle

import (
	"io"
	"time"

	"github.com/spacelions/j/internal/store/tasks"
)

// PlanLifecycle owns the begin/end task-log writes around a single
// agent.Plan invocation. The struct holds no bbolt handle — every
// task-log write goes through tasks.PersistWarn, which opens, writes,
// and closes within the same call so the bbolt file lock is never
// held across agent.Plan and a concurrent `j tasks` from another
// shell is not blocked. The lifecycle is constructed with NewPlanTask
// / BeginPlanReuse and finalised with Finish; callers pair them with
// a defer so the task is always written even when agent.Plan panics.
//
// agentLogPath is the per-task `agent.log` destination for phase
// markers; empty string disables marker emission (test paths).
type PlanLifecycle struct {
	stderr       io.Writer
	agentLogPath string
	task         tasks.Task
	closed       bool
}

// NewPlanTask records the "planning" entry for a real plan run. The
// caller passes the freshly-minted task id (so the per-task
// directory under <cwd>/.j/tasks/ uses the same id as the bbolt
// row), the markdown target the user is planning against (used for
// the basename fallback when the body has no usable first line), the
// requirement body, the plan-phase resume token (empty for agents
// with no notion of resume or on a NewResumeID failure already
// warned by the caller), and the agent.log path that phase markers
// should land in.
//
// Best effort: failure to open the task log or to write the initial
// row warns once on stderr and execution continues.
func NewPlanTask(stderr io.Writer, agentName, model, taskID, target, requirement, resumeID, agentLogPath, linearIssue string) *PlanLifecycle {
	task := tasks.Task{
		ID:                taskID,
		Status:            tasks.StatusPlanning,
		PlanTool:          agentName,
		PlanModel:         model,
		PlanResumeSession: resumeID,
		Summary:           tasks.Summary(requirement, target),
		PlanBeginAt:       time.Now().UTC(),
		LinearIssue:       linearIssue,
	}
	lc := &PlanLifecycle{stderr: stderr, agentLogPath: agentLogPath, task: task}
	tasks.PersistWarn(stderr, task)
	emitPhaseBegin(agentLogPath, "plan", task)
	return lc
}

// BeginPlanReuse mutates a copy of t to flip status to `planning`
// for the re-plan flow. PlanEndAt and DoneAt are cleared so the
// finalize step stamps fresh values; the original PlanBeginAt is
// preserved verbatim when set so the row keeps its first-run
// lineage. Tool/model and the plan resume session are refreshed so
// the row reflects the latest re-plan invocation.
//
// The body / source-path are intentionally not touched: re-plan
// reads requirements.md from the existing task directory and feeds
// it back through agent.Plan, so the summary derivation runs again
// in Finish.
func BeginPlanReuse(t tasks.Task, stderr io.Writer, agentName, model, resumeID, agentLogPath string) *PlanLifecycle {
	task := t
	task.Status = tasks.StatusPlanning
	task.PlanTool = agentName
	task.PlanModel = model
	task.PlanResumeSession = resumeID
	task.PlanEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.PlanBeginAt.IsZero() {
		task.PlanBeginAt = time.Now().UTC()
	}
	lc := &PlanLifecycle{stderr: stderr, agentLogPath: agentLogPath, task: task}
	tasks.PersistWarn(stderr, task)
	emitPhaseBegin(agentLogPath, "plan", task)
	return lc
}

// RecordBackground stamps the spawned child's PID and the agent log
// path on the in-memory task row and re-persists it. It is the
// counterpart of Finish for fire-and-forget headless runs: the row
// stays at status `planning` until the reaper in `j tasks` observes
// the child exited and finalises it.
//
// RecordBackground sets the closed flag so a defensive Finish fired
// by mistake (e.g. via a deferred guard) becomes a silent no-op and
// does not clobber the background row with `plan-done` / `help`.
func (lc *PlanLifecycle) RecordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	tasks.PersistWarn(lc.stderr, lc.task)
}

// Finish stamps plan_end_at, decides the terminal status from
// runErr, and (when runErr is nil) re-derives Summary from the
// refined requirements (then the plan body, then the file basename)
// because the agent may have rewritten the requirements during the
// session. The task is rewritten to the log even on errors so
// `help` is observable from `j tasks`. The bbolt store is opened
// just long enough to write the row and closed before this returns;
// calling Finish twice is a silent no-op via the closed flag.
func (lc *PlanLifecycle) Finish(runErr error, refinedRequirements, planMarkdown, target string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.PlanEndAt = time.Now().UTC()
	outcome := "done"
	if runErr != nil {
		lc.task.Status = tasks.StatusHelp
		outcome = "help"
	} else {
		lc.task.Status = tasks.StatusPlanDone
		lc.task.Summary = tasks.Summary(
			tasks.PickSource(refinedRequirements, planMarkdown), target)
	}
	tasks.PersistWarn(lc.stderr, lc.task)
	emitPhaseEnd(lc.agentLogPath, "plan", lc.task.PlanBeginAt, lc.task, outcome)
}

// Task returns the in-memory snapshot of the task row. The plan
// flow uses this for symmetry with WorkLifecycle.Task; the field is
// a value copy so callers cannot mutate the lifecycle's internal
// state.
func (lc *PlanLifecycle) Task() tasks.Task { return lc.task }
