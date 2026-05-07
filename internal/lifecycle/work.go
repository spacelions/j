package lifecycle

import (
	"io"
	"time"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// WorkLifecycle owns the begin/end task-log writes around a single
// agent.Work invocation. Mirrors PlanLifecycle.
type WorkLifecycle struct {
	stderr       io.Writer
	agentLogPath string
	task         tasks.Task
	prevStatus   tasks.TaskStatus
	closed       bool
}

// NewWorkTask records the "working" entry for a newly created work
// row. The caller has already minted the task id and staged the
// plan markdown into <cwd>/.j/tasks/<id>/plan.md.
func NewWorkTask(stderr io.Writer, agentName, model, taskID,
	planPath, requirement, planBody, resumeID, agentLogPath string,
) *WorkLifecycle {
	fromStatus := tasks.StatusPlanDone
	newStatus, err := tasks.Apply(fromStatus, tasks.EventWorkBegin)
	if err != nil {
		panic("work begin: " + err.Error())
	}
	task := tasks.Task{
		ID:                taskID,
		Status:            newStatus,
		WorkTool:          agentName,
		WorkModel:         model,
		WorkResumeSession: resumeID,
		Summary: tasks.FromPlanAndRequirement(
			requirement, planBody, planPath),
		WorkBeginAt: time.Now().UTC(),
	}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task, agentLogPath, fromStatus,
		tasks.EventWorkBegin)
}

// BeginWorkReuse mutates a copy of t to flip status to `working`.
func BeginWorkReuse(t tasks.Task, stderr io.Writer, agentName, model,
	resumeID, agentLogPath string,
) *WorkLifecycle {
	prev := t.Status
	newStatus, err := tasks.Apply(prev, tasks.EventWorkRestart)
	if err != nil {
		panic("work restart: " + err.Error())
	}
	task := t
	task.Status = newStatus
	task.WorkTool = agentName
	task.WorkModel = model
	task.WorkResumeSession = resumeID
	task.WorkBeginAt = time.Now().UTC()
	task.WorkEndAt = time.Time{}
	task.DoneAt = time.Time{}
	fillWorktree(&task)
	return openWorkLifecycle(stderr, task, agentLogPath, prev,
		tasks.EventWorkRestart)
}

// BeginWorkResume is the resume-flow companion of BeginWorkReuse.
func BeginWorkResume(t tasks.Task, stderr io.Writer,
	agentLogPath string,
) *WorkLifecycle {
	prev := t.Status
	newStatus, err := tasks.Apply(prev, tasks.EventWorkResume)
	if err != nil {
		panic("work resume: " + err.Error())
	}
	task := t
	task.Status = newStatus
	task.WorkEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.WorkBeginAt.IsZero() {
		task.WorkBeginAt = time.Now().UTC()
	}
	return openWorkLifecycle(stderr, task, agentLogPath, prev,
		tasks.EventWorkResume)
}

func openWorkLifecycle(stderr io.Writer, task tasks.Task,
	agentLogPath string, fromStatus tasks.TaskStatus,
	ev tasks.Event,
) *WorkLifecycle {
	task.AgentLogPath = agentLogPath
	lc := &WorkLifecycle{
		stderr:       stderr,
		agentLogPath: agentLogPath,
		task:         task,
		prevStatus:   task.Status,
	}
	tasks.PersistWarn(stderr, task)
	tasks.Notify(tasks.Transition{
		From: fromStatus, Event: ev, To: task.Status,
	}, task)
	return lc
}

func fillWorktree(task *tasks.Task) {
	if task.Worktree != "" {
		return
	}
	project, _ := store.ProjectName()
	task.Worktree = tasks.WorktreeNameFor(project, *task)
}

// RecordBackground stamps the spawned child's PID and the agent log
// path on the in-memory work task row and re-persists it.
func (lc *WorkLifecycle) RecordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	tasks.PersistWarn(lc.stderr, lc.task)
}

// Finish stamps work_end_at, picks the terminal status from runErr.
func (lc *WorkLifecycle) Finish(runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.WorkEndAt = time.Now().UTC()

	ev := tasks.EventWorkDone
	if runErr != nil {
		ev = tasks.EventWorkError
	}
	from := lc.task.Status
	newStatus, err := tasks.Apply(from, ev)
	if err != nil {
		panic("work finish: " + err.Error())
	}
	lc.task.Status = newStatus
	tasks.PersistWarn(lc.stderr, lc.task)
	tasks.Notify(tasks.Transition{
		From: from, Event: ev, To: newStatus,
	}, lc.task)
}

// Task returns the in-memory snapshot of the work task row.
func (lc *WorkLifecycle) Task() tasks.Task { return lc.task }
