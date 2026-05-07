package lifecycle

import (
	"io"
	"time"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// WorkLifecycle owns the begin/end task-log writes around a single
// agent.Work invocation. Mirrors PlanLifecycle: the struct holds no
// bbolt handle — every task-log write goes through tasks.PersistWarn
// so the bbolt file lock is never held across agent.Work and a
// concurrent `j tasks` from another shell is not blocked.
//
// Constructed with NewWorkTask, BeginWorkReuse, or BeginWorkResume
// depending on whether the run creates a new row, reuses an
// existing row, or resumes a prior work session.
//
// agentLogPath is the per-task `agent.log` destination for phase
// markers; empty string disables marker emission (test paths).
type WorkLifecycle struct {
	stderr       io.Writer
	agentLogPath string
	task         tasks.Task
	closed       bool
}

// NewWorkTask records the "working" entry for a newly created work
// row. The caller has already minted the task id and staged the
// plan markdown into <cwd>/.j/tasks/<id>/plan.md (and optionally
// requirements.md). This helper just stamps the bbolt row.
//
// Worktree is minted via tasks.WorktreeNameFor so the worker and
// the verifier share one rule; callers that pre-populate Worktree
// (none today — `j plan` does not set it) still have their value
// preserved.
func NewWorkTask(stderr io.Writer, agentName, model, taskID, planPath, requirement, planBody, resumeID, agentLogPath string) *WorkLifecycle {
	task := tasks.Task{
		ID:                taskID,
		Status:            tasks.StatusWorking,
		WorkTool:          agentName,
		WorkModel:         model,
		WorkResumeSession: resumeID,
		Summary:           tasks.FromPlanAndRequirement(requirement, planBody, planPath),
		WorkBeginAt:       time.Now().UTC(),
	}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task, agentLogPath)
}

// BeginWorkReuse mutates a copy of t to flip status to `working`,
// stamp work_begin_at, clear stale work_end_at / done_at from a
// previous failed run, and record the latest tool/model and resume
// session for the work phase. Plan-phase fields are preserved.
//
// A pre-existing Worktree on t is kept verbatim (so manual edits
// persist); an empty one is populated via tasks.WorktreeNameFor so
// rows that pre-date the field still gain a meaningful name on
// their first bbolt-sourced `j work`.
func BeginWorkReuse(t tasks.Task, stderr io.Writer, agentName, model, resumeID, agentLogPath string) *WorkLifecycle {
	task := t
	task.Status = tasks.StatusWorking
	task.WorkTool = agentName
	task.WorkModel = model
	task.WorkResumeSession = resumeID
	task.WorkBeginAt = time.Now().UTC()
	task.WorkEndAt = time.Time{}
	task.DoneAt = time.Time{}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task, agentLogPath)
}

// BeginWorkResume is the resume-flow companion of BeginWorkReuse.
// The two functions diverge in two places:
//
//  1. The existing WorkResumeSession is preserved verbatim instead
//     of being overwritten with a fresh `Agent.NewResumeID` value
//     (the whole point of resume is reusing the session recorded on
//     the task row).
//  2. The original WorkBeginAt timestamp is preserved when set so
//     the task row keeps its first-run lineage; only WorkEndAt /
//     DoneAt are cleared so Finish stamps fresh values on the next
//     finalize. Tool/model are kept verbatim because resume never
//     re-prompts the user for them.
func BeginWorkResume(t tasks.Task, stderr io.Writer, agentLogPath string) *WorkLifecycle {
	task := t
	task.Status = tasks.StatusWorking
	task.WorkEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.WorkBeginAt.IsZero() {
		task.WorkBeginAt = time.Now().UTC()
	}
	return openWorkLifecycle(stderr, task, agentLogPath)
}

// openWorkLifecycle is the shared helper that best-effort writes
// the initial row and returns a WorkLifecycle suitable for Finish.
func openWorkLifecycle(stderr io.Writer, task tasks.Task, agentLogPath string) *WorkLifecycle {
	lc := &WorkLifecycle{stderr: stderr, agentLogPath: agentLogPath, task: task}
	tasks.PersistWarn(stderr, task)
	emitPhaseBegin(agentLogPath, "work", task)
	return lc
}

// fillWorktree populates task.Worktree via tasks.WorktreeNameFor
// when it is empty, leaving a pre-existing value untouched. A
// ProjectName lookup failure (cwd removed while the process runs)
// is treated as "no project slug" so the helper still mints a
// task-only slug instead of bailing: `j work` has more important
// things to do than surface a hard error for a cosmetic worktree
// label.
func fillWorktree(task *tasks.Task) {
	if task.Worktree != "" {
		return
	}
	project, _ := store.ProjectName()
	task.Worktree = tasks.WorktreeNameFor(project, *task)
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
	tasks.PersistWarn(lc.stderr, lc.task)
}

// Finish stamps work_end_at, picks the terminal status from runErr
// (work-done on success, help on error), and rewrites the task. The
// `completed` status (and DoneAt) is reserved for `j verify`;
// `j work` no longer terminates the lifecycle here. Calling Finish
// twice is a silent no-op via the closed flag.
func (lc *WorkLifecycle) Finish(runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.WorkEndAt = time.Now().UTC()
	outcome := "done"
	if runErr != nil {
		lc.task.Status = tasks.StatusHelp
		outcome = "help"
	} else {
		lc.task.Status = tasks.StatusWorkDone
	}
	tasks.PersistWarn(lc.stderr, lc.task)
	emitPhaseEnd(lc.agentLogPath, "work", lc.task.WorkBeginAt, lc.task, outcome)
}

// Task returns the in-memory snapshot of the work task row. Used
// by `j work` to read the freshly-minted Worktree value for the
// agent.Work request without poking at the unexported struct field.
func (lc *WorkLifecycle) Task() tasks.Task { return lc.task }
