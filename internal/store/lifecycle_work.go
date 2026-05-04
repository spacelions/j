package store

import (
	"io"
	"time"
)

// WorkLifecycle owns the begin/end task-log writes around a single
// agent.Work invocation. Mirrors PlanLifecycle: the struct holds no
// bbolt handle — every task-log write goes through PersistWarn so
// the bbolt file lock is never held across agent.Work and a
// concurrent `j tasks` from another shell is not blocked.
//
// Constructed with NewWorkTask, Task.BeginWorkReuse, or
// Task.BeginWorkResume depending on whether the run creates a new row,
// reuses an existing row, or resumes a prior work session.
type WorkLifecycle struct {
	stderr io.Writer
	task   Task
	closed bool
}

// NewWorkTask records the "working" entry for a newly created work row.
// The caller has already minted the task id and staged the
// plan markdown into <cwd>/.j/tasks/<id>/plan.md (and optionally
// requirements.md). This helper just stamps the bbolt row.
//
// Worktree is minted via WorktreeNameFor so the worker and the
// verifier share one rule; callers that pre-populate Worktree (none
// today — `j plan` does not set it) still have their value preserved.
func NewWorkTask(stderr io.Writer, agentName, model, taskID, planPath, requirement, planBody, resumeID string) *WorkLifecycle {
	begin := time.Now().UTC()
	task := Task{
		ID:               taskID,
		Status:           StatusWorking,
		InvokedTool:      agentName,
		InvokedModel:     model,
		WorkResumeCursor: resumeID,
		Summary:          FromPlanAndRequirement(requirement, planBody, planPath),
		WorkBeginAt:      &begin,
	}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task)
}

// BeginWorkReuse mutates a copy of the receiver to flip status to
// `working`, stamp work_begin_at, clear stale work_end_at / done_at
// from a previous failed run, and record the latest tool/model and
// resume cursor for the work phase. Plan-phase fields are preserved.
//
// A pre-existing Worktree on the receiver is kept verbatim (so manual
// edits persist); an empty one is populated via WorktreeNameFor so
// rows that pre-date the field still gain a meaningful name on their
// first bbolt-sourced `j work`.
func (t Task) BeginWorkReuse(stderr io.Writer, agentName, model, resumeID string) *WorkLifecycle {
	begin := time.Now().UTC()
	task := t
	task.Status = StatusWorking
	task.InvokedTool = agentName
	task.InvokedModel = model
	task.WorkResumeCursor = resumeID
	task.WorkBeginAt = &begin
	task.WorkEndAt = nil
	task.DoneAt = nil
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task)
}

// BeginWorkResume is the resume-flow companion of BeginWorkReuse. The
// two functions diverge in two places:
//
//  1. The existing WorkResumeCursor is preserved verbatim instead of
//     being overwritten with a fresh `Agent.NewResumeID` value (the
//     whole point of resume is reusing the cursor recorded on the
//     task row).
//  2. The original WorkBeginAt timestamp is preserved when set so the
//     task row keeps its first-run lineage; only WorkEndAt / DoneAt
//     are cleared so Finish stamps fresh values on the next finalize.
//     Tool/model are kept verbatim because resume never re-prompts
//     the user for them.
func (t Task) BeginWorkResume(stderr io.Writer) *WorkLifecycle {
	task := t
	task.Status = StatusWorking
	task.WorkEndAt = nil
	task.DoneAt = nil
	if task.WorkBeginAt == nil {
		begin := time.Now().UTC()
		task.WorkBeginAt = &begin
	}
	return openWorkLifecycle(stderr, task)
}

// openWorkLifecycle is the shared helper that best-effort writes the
// initial row and returns a WorkLifecycle suitable for Finish.
func openWorkLifecycle(stderr io.Writer, task Task) *WorkLifecycle {
	lc := &WorkLifecycle{stderr: stderr, task: task}
	PersistWarn(stderr, task)
	emitPhaseBegin(stderr, "work", task)
	return lc
}

// fillWorktree populates task.Worktree via WorktreeNameFor when it is
// empty, leaving a pre-existing value untouched. A ProjectName lookup
// failure (cwd removed while the process runs) is treated as "no
// project slug" so the helper still mints a task-only slug instead
// of bailing: `j work` has more important things to do than surface
// a hard error for a cosmetic worktree label.
func fillWorktree(task *Task) {
	if task.Worktree != "" {
		return
	}
	project, _ := ProjectName()
	task.Worktree = WorktreeNameFor(project, *task)
}

// RecordBackground stamps the spawned child's PID and the agent log
// path on the in-memory work task row and re-persists it. The row
// stays at status `working` until the reaper in `j tasks` observes
// the child exited and finalises it.
//
// RecordBackground sets the closed flag so a defensive Finish fired
// by mistake becomes a silent no-op and does not clobber the
// background row with `work-done` / `help`.
func (lc *WorkLifecycle) RecordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	PersistWarn(lc.stderr, lc.task)
}

// Finish stamps work_end_at, picks the terminal status from runErr
// (work-done on success, help on error), and rewrites the task. The
// `completed` status (and DoneAt) is reserved for `j verify`; `j work`
// no longer terminates the lifecycle here. Calling Finish twice is a
// silent no-op via the closed flag.
func (lc *WorkLifecycle) Finish(runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.WorkEndAt = &end
	outcome := "done"
	if runErr != nil {
		lc.task.Status = StatusHelp
		outcome = "help"
	} else {
		lc.task.Status = StatusWorkDone
	}
	PersistWarn(lc.stderr, lc.task)
	emitPhaseEnd(lc.stderr, "work", lc.task.WorkBeginAt, lc.task, outcome)
}

// Task returns the in-memory snapshot of the work task row. Used by
// `j work` to read the freshly-minted Worktree value for the
// agent.Work request without poking at the unexported struct field.
func (lc *WorkLifecycle) Task() Task { return lc.task }
