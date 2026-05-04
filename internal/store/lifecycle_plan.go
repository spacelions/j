package store

import (
	"io"
	"time"
)

// PlanLifecycle owns the begin/end task-log writes around a single
// agent.Plan invocation. The struct holds no bbolt handle — every
// task-log write goes through PersistWarn, which opens, writes, and
// closes within the same call so the bbolt file lock is never held
// across agent.Plan and a concurrent `j tasks` from another shell is
// not blocked. The lifecycle is constructed with NewPlanTask /
// Task.BeginPlanReuse and finalised with Finish; callers pair them
// with a defer so the task is always written even when agent.Plan
// panics.
type PlanLifecycle struct {
	stderr io.Writer
	task   Task
	closed bool
}

// NewPlanTask records the "planning" entry for a real plan run. The
// caller passes the freshly-minted task id (so the per-task directory
// under <cwd>/.j/tasks/ uses the same id as the bbolt row), the
// markdown target the user is planning against (used for the basename
// fallback when the body has no usable first line), the requirement
// body, and the plan-phase resume token (empty for agents with no
// notion of resume or on a NewResumeID failure already warned by the
// caller).
//
// Best effort: failure to open the task log or to write the initial
// row warns once on stderr and execution continues.
func NewPlanTask(stderr io.Writer, agentName, model, taskID, target, requirement, resumeID string) *PlanLifecycle {
	begin := time.Now().UTC()
	task := Task{
		ID:               taskID,
		Status:           StatusPlanning,
		InvokedTool:      agentName,
		InvokedModel:     model,
		PlanResumeCursor: resumeID,
		Summary:          Summary(requirement, target),
		PlanBeginAt:      &begin,
	}
	lc := &PlanLifecycle{stderr: stderr, task: task}
	PersistWarn(stderr, task)
	return lc
}

// BeginPlanReuse mutates a copy of the receiver to flip status to
// `planning` for the re-plan flow. PlanEndAt and DoneAt are cleared
// so the finalize step stamps fresh values; the original
// PlanBeginAt is preserved verbatim when set so the row keeps its
// first-run lineage. Tool/model and the plan resume cursor are
// refreshed so the row reflects the latest re-plan invocation.
//
// The body / source-path are intentionally not touched: re-plan
// reads requirements.md from the existing task directory and feeds
// it back through agent.Plan, so the summary derivation runs again
// in Finish.
func (t Task) BeginPlanReuse(stderr io.Writer, agentName, model, resumeID string) *PlanLifecycle {
	begin := time.Now().UTC()
	task := t
	task.Status = StatusPlanning
	task.InvokedTool = agentName
	task.InvokedModel = model
	task.PlanResumeCursor = resumeID
	task.PlanEndAt = nil
	task.DoneAt = nil
	if task.PlanBeginAt == nil {
		task.PlanBeginAt = &begin
	}
	lc := &PlanLifecycle{stderr: stderr, task: task}
	PersistWarn(stderr, task)
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
	PersistWarn(lc.stderr, lc.task)
}

// Finish stamps plan_end_at, decides the terminal status from runErr,
// and (when runErr is nil) re-derives Summary from the refined
// requirements (then the plan body, then the file basename) because
// the agent may have rewritten the requirements during the session.
// The task is rewritten to the log even on errors so `help` is
// observable from `j tasks`. The bbolt store is opened just long
// enough to write the row and closed before this returns; calling
// Finish twice is a silent no-op via the closed flag.
func (lc *PlanLifecycle) Finish(runErr error, refinedRequirements, planMarkdown, target string) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.PlanEndAt = &end
	if runErr != nil {
		lc.task.Status = StatusHelp
	} else {
		lc.task.Status = StatusPlanDone
		lc.task.Summary = Summary(PickSource(refinedRequirements, planMarkdown), target)
	}
	PersistWarn(lc.stderr, lc.task)
}

// Task returns the in-memory snapshot of the task row. The plan flow
// uses this for symmetry with WorkLifecycle.Task; the field is a
// value copy so callers cannot mutate the lifecycle's internal state.
func (lc *PlanLifecycle) Task() Task { return lc.task }
