package plan

import (
	"io"
	"time"

	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// planLifecycle owns the begin/end task-log writes around a single
// agent.Plan invocation. The struct holds no bbolt handle — every
// task-log write goes through tasklog.PersistWarn, which opens
// `<cwd>/.j/tasks/list.db`, writes, and closes within the same
// call so the bbolt file lock is never held across agent.Plan and
// a concurrent `j tasks` from another shell is not blocked. The
// lifecycle is constructed with beginPlanTask and finalised with
// finishPlan; callers pair them with a defer so the task is always
// written even when agent.Plan panics.
type planLifecycle struct {
	stderr io.Writer
	task   store.Task
	closed bool
}

// beginPlanTask records the "planning" entry for a real plan run.
//   - taskID: minted by the caller so the per-task directory under
//     <cwd>/.j/tasks/ uses the same id as the bbolt row.
//   - target: the markdown file the user is planning against. The
//     summary is derived from the body when possible and falls back
//     to the file basename so `j tasks` never shows a blank summary.
//   - requirement: the markdown body the user is planning from.
//   - planResumeChatID: per-phase resume token from
//     Agent.NewResumeID; empty for agents with no notion of resume
//     or on a NewResumeID failure (already warned by the caller).
//
// Best effort: failure to open the task log or to write the initial
// row warns once on stderr and execution continues. The on-disk
// pre-flight (`j init`) has already laid down `.j/tasks/list.db`, so
// the open call is read/write only and never creates new files.
func beginPlanTask(opts Options, agent codingagents.Agent, model, taskID, target, requirement, planResumeChatID string) *planLifecycle {
	begin := time.Now().UTC()
	task := store.Task{
		ID:               taskID,
		Status:           store.StatusPlanning,
		InvokedTool:      agent.Name(),
		InvokedModel:     model,
		PlanResumeCursor: planResumeChatID,
		Summary:          tasklog.Summary(requirement, target),
		PlanBeginAt:      &begin,
	}
	lc := &planLifecycle{stderr: opts.Stderr, task: task}
	tasklog.PersistWarn(opts.Stderr, task)
	return lc
}

// beginPlanTaskReuse mutates a copy of `existing` to flip status to
// `planning` for the re-plan flow. PlanEndAt and DoneAt are cleared
// so the finalize step stamps fresh values; the original
// PlanBeginAt is preserved verbatim when set so the row keeps its
// first-run lineage. Tool/model and the plan resume cursor are
// refreshed so the row reflects the latest re-plan invocation.
//
// The body / source-path are intentionally not touched: re-plan
// reads requirements.md from the existing task directory and feeds
// it back through agent.Plan, so the summary derivation runs again
// in finishPlan.
func beginPlanTaskReuse(opts Options, agent codingagents.Agent, model string, existing store.Task, planResumeChatID string) *planLifecycle {
	begin := time.Now().UTC()
	task := existing
	task.Status = store.StatusPlanning
	task.InvokedTool = agent.Name()
	task.InvokedModel = model
	task.PlanResumeCursor = planResumeChatID
	task.PlanEndAt = nil
	task.DoneAt = nil
	if task.PlanBeginAt == nil {
		task.PlanBeginAt = &begin
	}
	lc := &planLifecycle{stderr: opts.Stderr, task: task}
	tasklog.PersistWarn(opts.Stderr, task)
	return lc
}

// recordBackground stamps the spawned child's PID and the agent log
// path on the in-memory task row and re-persists it. It is the
// counterpart of finishPlan for fire-and-forget headless runs: the
// row stays at status `planning` until the reaper in `j tasks`
// observes the child exited and finalises it.
//
// recordBackground sets the closed flag so a defensive finishPlan
// fired by mistake (e.g. via a deferred guard) becomes a silent
// no-op and does not clobber the background row with `plan-done` /
// `help`.
func (lc *planLifecycle) recordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	tasklog.PersistWarn(lc.stderr, lc.task)
}

// finishPlan stamps plan_end_at, decides the terminal status from
// runErr, and (when planErr is nil) re-derives Summary from the
// refined requirements (then the plan body, then the file basename)
// because the agent may have rewritten the requirements during the
// session. The task is rewritten to the log even on errors so `help`
// is observable from `j tasks`. The bbolt store is opened just long
// enough to write the row and closed before this returns; calling
// finishPlan twice is a silent no-op via the closed flag.
func (lc *planLifecycle) finishPlan(runErr error, refinedRequirements, planMarkdown, target string) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.PlanEndAt = &end
	if runErr != nil {
		lc.task.Status = store.StatusHelp
	} else {
		lc.task.Status = store.StatusPlanDone
		lc.task.Summary = tasklog.Summary(tasklog.PickSource(refinedRequirements, planMarkdown), target)
	}
	tasklog.PersistWarn(lc.stderr, lc.task)
}
