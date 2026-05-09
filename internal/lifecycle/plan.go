// Package lifecycle owns the per-phase begin/end task-log writes
// and the planner → worker → verifier orchestration that drives
// `j tasks` end to end. The per-phase helpers (NewPlanTask,
// BeginWorkRestart, BeginVerifyResume, ...) live in this package's
// root; the SequentialAgent / launcher wiring lives in
// internal/lifecycle/orchestrator/ to avoid an import cycle with
// internal/agents/{planner,worker,verifier}, which in turn call back
// into the per-phase helpers here.
package lifecycle

import (
	"io"
	"time"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// PlanLifecycle owns the begin/end task-log writes around a single
// agent.Plan invocation. The struct holds no bbolt handle — every
// task-log write goes through tasks.PersistWarn, which opens, writes,
// and closes within the same call so the bbolt file lock is never
// held across agent.Plan and a concurrent `j tasks` from another
// shell is not blocked. The lifecycle is constructed with NewPlanTask
// / BeginPlanRestart and finalised with Finish; callers pair them with
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
func NewPlanTask(stderr io.Writer, agentName, model, taskID, target,
	requirement, resumeID, agentLogPath, linearIssue string,
) *PlanLifecycle {
	task := tasks.Task{
		ID:                taskID,
		PlanTool:          agentName,
		PlanModel:         model,
		PlanResumeSession: resumeID,
		Summary:           tasks.Summary(requirement, target),
		PlanBeginAt:       time.Now().UTC(),
		LinearIssue:       linearIssue,
		AgentLogPath:      agentLogPath,
	}
	if _, err := tasks.ApplyAndPersistWarn(
		stderr, &task, tasks.EventPlanBegin); err != nil {
		panic("plan begin from zero value: " + err.Error())
	}
	return &PlanLifecycle{
		stderr:       stderr,
		agentLogPath: agentLogPath,
		task:         task,
	}
}

// BeginPlanRestart mutates a copy of t to flip status to `planning`
// for the re-plan flow. PlanEndAt and DoneAt are cleared so the
// finalize step stamps fresh values; the original PlanBeginAt is
// preserved verbatim when set so the row keeps its first-run
// lineage. Tool/model and the plan resume session are refreshed so
// the row reflects the latest re-plan invocation.
func BeginPlanRestart(t tasks.Task, stderr io.Writer, agentName, model,
	resumeID, agentLogPath string,
) *PlanLifecycle {
	task := t
	task.PlanTool = agentName
	task.PlanModel = model
	task.PlanResumeSession = resumeID
	task.PlanEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.PlanBeginAt.IsZero() {
		task.PlanBeginAt = time.Now().UTC()
	}
	task.AgentLogPath = agentLogPath
	if _, err := tasks.ApplyAndPersistWarn(
		stderr, &task, tasks.EventPlanRestart); err != nil {
		panic("plan restart: " + err.Error())
	}
	return &PlanLifecycle{
		stderr:       stderr,
		agentLogPath: agentLogPath,
		task:         task,
	}
}

// BeginPlanResume mirrors BeginPlanRestart but is the resume-flow
// companion: the row's existing PlanResumeSession is preserved
// verbatim (so the backend forwards the original `--resume <id>` to
// the underlying CLI) and the FSM transition is EventPlanResume so
// notify hooks see the resume edge instead of a restart. Tool/model
// are refreshed because a resume can switch backends just like a
// re-plan can; PlanEndAt / DoneAt are cleared so Finish stamps fresh
// values, while PlanBeginAt is preserved when set so the row keeps
// its first-run lineage.
func BeginPlanResume(t tasks.Task, stderr io.Writer, agentName, model,
	agentLogPath string,
) *PlanLifecycle {
	prev := t.Status
	newStatus, err := tasks.Apply(prev, tasks.EventPlanResume)
	if err != nil {
		panic("plan resume: " + err.Error())
	}
	task := t
	task.Status = newStatus
	task.PlanTool = agentName
	task.PlanModel = model
	task.PlanEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.PlanBeginAt.IsZero() {
		task.PlanBeginAt = time.Now().UTC()
	}
	task.AgentLogPath = agentLogPath
	lc := &PlanLifecycle{
		stderr:       stderr,
		agentLogPath: agentLogPath,
		task:         task,
	}
	tasks.PersistWarn(stderr, task)
	tasks.Notify(tasks.Transition{
		From: prev, Event: tasks.EventPlanResume, To: newStatus,
	}, task)
	return lc
}

// BeginPlanExisting creates a PlanLifecycle for a task that is
// already at `planning` — the seed row was written by persistStartRow
// and the planner just needs to run the plan phase without a status
// transition.
func BeginPlanExisting(t tasks.Task, stderr io.Writer, agentName,
	model, resumeID, agentLogPath string,
) *PlanLifecycle {
	task := t
	task.PlanTool = agentName
	task.PlanModel = model
	task.PlanResumeSession = resumeID
	task.AgentLogPath = agentLogPath
	if task.PlanBeginAt.IsZero() {
		task.PlanBeginAt = time.Now().UTC()
	}
	lc := &PlanLifecycle{
		stderr:       stderr,
		agentLogPath: agentLogPath,
		task:         task,
	}
	tasks.PersistWarn(stderr, task)
	return lc
}

// RecordAgentLog stamps the per-task agent.log path on the in-memory
// task row and re-persists it. The lifecycle is closed afterwards so
// Finish becomes a no-op — the orchestrator hand-off to a detached
// child is the terminal event for this lifecycle's bookkeeping. The
// pid field that used to live alongside this method was retired in
// SPA-72: the per-task `flock` is now the source of truth for "who is
// holding the row".
func (lc *PlanLifecycle) RecordAgentLog(logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.AgentLogPath = logPath
	tasks.PersistWarn(lc.stderr, lc.task)
}

// RecordResumeSession stamps id onto the in-memory task row's
// PlanResumeSession field and re-persists. Used for the post-run
// capture path (deepseek-tui has no pre-run session-id binding flag,
// so the orchestrator captures the id after the first turn writes
// to disk and threads it back here so a later resume run finds it).
// A no-op when id is empty so call sites do not need to gate the
// helper themselves. Does NOT close the lifecycle: Finish must still
// run to stamp the terminal status.
func (lc *PlanLifecycle) RecordResumeSession(id string) {
	if id == "" {
		return
	}
	lc.task.PlanResumeSession = id
	tasks.PersistWarn(lc.stderr, lc.task)
}

// Finish stamps plan_end_at, decides the terminal status from
// runErr, the on-disk clarification.md, and the plan-requires-approval
// setting, and rewrites the task. Calling Finish twice is a silent
// no-op via the closed flag. A clean run that wrote `clarification.md`
// (planner contract for "I need a human") routes to
// `needs-clarification` instead of `plan-done` so linear_push.go does
// not try to upload a missing plan.md. runErr always wins over the
// clarification check.
func (lc *PlanLifecycle) Finish(
	runErr error, refinedRequirements, planMarkdown, target string,
) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.PlanEndAt = time.Now().UTC()

	ev := lc.pickFinishEvent(runErr, refinedRequirements, planMarkdown,
		target)
	if _, err := tasks.ApplyAndPersistWarn(
		lc.stderr, &lc.task, ev); err != nil {
		panic("plan finish: " + err.Error())
	}
}

// pickFinishEvent decides which event drives the plan-finish
// transition. Error path takes precedence over the clarification
// check; clarification.md presence wins over the approval gate.
// Summary is only refreshed on the plan-done / plan-await-approval
// branches — the clarification branch leaves the begin-time summary
// alone because refined inputs are typically empty there.
func (lc *PlanLifecycle) pickFinishEvent(
	runErr error, refinedRequirements, planMarkdown, target string,
) tasks.Event {
	if runErr != nil {
		return tasks.EventPlanError
	}
	if taskClarificationPresent(lc.task.ID) {
		return tasks.EventPlanNeedsClarification
	}
	lc.task.Summary = tasks.Summary(
		tasks.PickSource(refinedRequirements, planMarkdown), target)
	approval, _ := store.LoadPlanRequiresApproval()
	if approval {
		return tasks.EventPlanAwaitApproval
	}
	return tasks.EventPlanDone
}

// Task returns the in-memory snapshot of the task row.
func (lc *PlanLifecycle) Task() tasks.Task { return lc.task }
