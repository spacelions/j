package work

import (
	"io"
	"time"

	"github.com/spacelions/j/internal/cli/tasklog"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// workLifecycle owns the begin/end task-log writes around a single
// agent.Work invocation. The struct holds no bbolt handle — every
// task-log write goes through tasklog.PersistWarn, which opens
// `<cwd>/.j/tasks/list.db`, writes, and closes within the same
// call so the bbolt file lock is never held across agent.Work and a
// concurrent `j tasks` from another shell is not blocked.
//
// The struct is constructed with one of beginWorkTaskNew,
// beginWorkTaskReuse, or beginWorkTaskResume depending on whether the
// run is a legacy file import (creates a new bbolt row), a
// bbolt-sourced run (mutates an existing row in place), or a resume.
// finishWork is shared.
type workLifecycle struct {
	stderr io.Writer
	task   store.Task
	closed bool
}

// beginWorkTaskNew records the "working" entry for a legacy
// `--from-file` import. The caller has already minted the task id and
// staged the plan markdown into <cwd>/.j/tasks/<id>/plan.md (and
// optionally requirements.md). This helper just stamps the bbolt row.
//
// Worktree is minted via store.WorktreeNameFor so the worker and the
// verifier share one rule; callers that pre-populate Worktree (none
// today — `j plan` does not set it) still have their value preserved.
func beginWorkTaskNew(opts Options, agent codingagents.Agent, model, taskID, planPath, requirement, planBody, workResumeChatID string) *workLifecycle {
	begin := time.Now().UTC()
	task := store.Task{
		ID:               taskID,
		Status:           store.StatusWorking,
		InvokedTool:      agent.Name(),
		InvokedModel:     model,
		WorkResumeCursor: workResumeChatID,
		Summary:          tasklog.FromPlanAndRequirement(requirement, planBody, planPath),
		WorkBeginAt:      &begin,
	}
	fillWorktree(&task)
	return openLifecycle(opts, task)
}

// beginWorkTaskReuse mutates a copy of `existing` to flip status to
// `working`, stamp work_begin_at, clear stale work_end_at / done_at
// from a previous failed run, and record the latest tool/model and
// resume cursor for the work phase. Plan-phase fields are preserved.
//
// A pre-existing Worktree on `existing` is kept verbatim (so manual
// edits persist); an empty one is populated via store.WorktreeNameFor
// so rows that pre-date the field still gain a meaningful name on
// their first bbolt-sourced `j work`.
func beginWorkTaskReuse(opts Options, agent codingagents.Agent, model string, existing store.Task, workResumeChatID string) *workLifecycle {
	begin := time.Now().UTC()
	task := existing
	task.Status = store.StatusWorking
	task.InvokedTool = agent.Name()
	task.InvokedModel = model
	task.WorkResumeCursor = workResumeChatID
	task.WorkBeginAt = &begin
	task.WorkEndAt = nil
	task.DoneAt = nil
	fillWorktree(&task)
	return openLifecycle(opts, task)
}

// beginWorkTaskResume is the resume-flow companion of
// beginWorkTaskReuse. The two functions diverge in two places:
//
//  1. The existing WorkResumeCursor is preserved verbatim instead
//     of being overwritten with a fresh `Agent.NewResumeID` value
//     (the whole point of resume is reusing the cursor recorded on
//     the task row).
//  2. The original WorkBeginAt timestamp is preserved when set so
//     the task row keeps its first-run lineage; only WorkEndAt /
//     DoneAt are cleared so finishWork stamps fresh values on the
//     next finalize. Tool/model are kept verbatim because resume
//     never re-prompts the user for them.
func beginWorkTaskResume(opts Options, existing store.Task) *workLifecycle {
	task := existing
	task.Status = store.StatusWorking
	task.WorkEndAt = nil
	task.DoneAt = nil
	if task.WorkBeginAt == nil {
		begin := time.Now().UTC()
		task.WorkBeginAt = &begin
	}
	return openLifecycle(opts, task)
}

// openLifecycle is the shared helper that best-effort writes the
// initial row and returns a workLifecycle suitable for finishWork.
// The bbolt handle is opened, written to, and closed within
// tasklog.PersistWarn so the file lock is not held across
// agent.Work. Pre-flight has already laid down `.j/tasks/list.db`,
// so the open call is read/write only.
func openLifecycle(opts Options, task store.Task) *workLifecycle {
	lc := &workLifecycle{stderr: opts.Stderr, task: task}
	tasklog.PersistWarn(opts.Stderr, task)
	return lc
}

// fillWorktree populates task.Worktree via store.WorktreeNameFor when
// it is empty, leaving a pre-existing value untouched. A ProjectName
// lookup failure (cwd removed while the process runs) is treated as
// "no project slug" so the helper still mints a task-only slug
// instead of bailing: `j work` has more important things to do than
// surface a hard error for a cosmetic worktree label.
func fillWorktree(task *store.Task) {
	if task.Worktree != "" {
		return
	}
	project, _ := store.ProjectName()
	task.Worktree = store.WorktreeNameFor(project, *task)
}

// recordBackground stamps the spawned child's PID and the agent log
// path on the in-memory work task row and re-persists it. It is the
// counterpart of finishWork for fire-and-forget headless runs: the
// row stays at status `working` until the reaper in `j tasks`
// observes the child exited and finalises it.
//
// recordBackground sets the closed flag so a defensive finishWork
// fired by mistake (e.g. via a deferred guard) becomes a silent
// no-op and does not clobber the background row with `work-done` /
// `help`.
func (lc *workLifecycle) recordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	tasklog.PersistWarn(lc.stderr, lc.task)
}

// finishWork stamps work_end_at, picks the terminal status from runErr
// (work-done on success, help on error), and rewrites the task. The
// `completed` status (and DoneAt) is reserved for a future `j verify`
// command; `j work` no longer terminates the lifecycle here. The
// bbolt store is opened just long enough to write the row and closed
// before this returns; calling finishWork twice is a silent no-op
// via the closed flag.
func (lc *workLifecycle) finishWork(runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.WorkEndAt = &end
	if runErr != nil {
		lc.task.Status = store.StatusHelp
	} else {
		lc.task.Status = store.StatusWorkDone
	}
	tasklog.PersistWarn(lc.stderr, lc.task)
}
