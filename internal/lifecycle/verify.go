package lifecycle

import (
	"io"
	"time"

	"github.com/spacelions/j/internal/store/tasks"
)

// VerifyOutcome enumerates the terminal results of `j verify`'s
// fix-loop.
type VerifyOutcome int

const (
	VerifyOutcomeSuccess    VerifyOutcome = iota
	VerifyOutcomeNoRetries
)

// VerifyLifecycle owns the begin/end task-log writes around a
// single `j verify` invocation.
type VerifyLifecycle struct {
	stderr       io.Writer
	agentLogPath string
	task         tasks.Task
	prevStatus   tasks.TaskStatus
	closed       bool
}

// BeginVerify flips an existing task row to `verifying`.
func BeginVerify(t tasks.Task, stderr io.Writer, agentName, model,
	resumeID, agentLogPath string,
) *VerifyLifecycle {
	prev := t.Status
	newStatus, err := tasks.Apply(prev, tasks.EventVerifyBegin)
	if err != nil {
		panic("verify begin: " + err.Error())
	}
	task := t
	task.Status = newStatus
	task.VerifyTool = agentName
	task.VerifyModel = model
	task.VerifyResumeSession = resumeID
	task.VerifyBeginAt = time.Now().UTC()
	task.VerifyEndAt = time.Time{}
	task.DoneAt = time.Time{}
	return openVerifyLifecycle(stderr, task, agentLogPath, prev,
		tasks.EventVerifyBegin)
}

// BeginVerifyResume is the resume-flow companion of BeginVerify.
func BeginVerifyResume(t tasks.Task, stderr io.Writer,
	agentLogPath string,
) *VerifyLifecycle {
	prev := t.Status
	newStatus, err := tasks.Apply(prev, tasks.EventVerifyResume)
	if err != nil {
		panic("verify resume: " + err.Error())
	}
	task := t
	task.Status = newStatus
	task.VerifyEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.VerifyBeginAt.IsZero() {
		task.VerifyBeginAt = time.Now().UTC()
	}
	return openVerifyLifecycle(stderr, task, agentLogPath, prev,
		tasks.EventVerifyResume)
}

func openVerifyLifecycle(stderr io.Writer, task tasks.Task,
	agentLogPath string, fromStatus tasks.TaskStatus,
	ev tasks.Event,
) *VerifyLifecycle {
	task.AgentLogPath = agentLogPath
	lc := &VerifyLifecycle{
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

// RecordBackground stamps PID + log path on the verify task row.
func (lc *VerifyLifecycle) RecordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	tasks.PersistWarn(lc.stderr, lc.task)
}

// Finish stamps verify_end_at and picks the terminal status from
// (outcome, runErr).
func (lc *VerifyLifecycle) Finish(outcome VerifyOutcome, runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.VerifyEndAt = time.Now().UTC()

	var ev tasks.Event
	switch {
	case runErr != nil:
		ev = tasks.EventVerifyError
	case outcome == VerifyOutcomeSuccess:
		ev = tasks.EventVerifyPass
	default:
		ev = tasks.EventVerifyFail
	}
	from := lc.task.Status
		newStatus, err := tasks.Apply(from, ev)
	if err != nil {
		panic("verify finish: " + err.Error())
	}
	lc.task.Status = newStatus
	if newStatus == tasks.StatusCompleted {
		lc.task.DoneAt = time.Now().UTC()
	}
	tasks.PersistWarn(lc.stderr, lc.task)
	tasks.Notify(tasks.Transition{
		From: from, Event: ev, To: newStatus,
	}, lc.task)
}

// IterationBegin writes one verify_iteration_begin marker.
func (lc *VerifyLifecycle) IterationBegin(iteration, max int) {
	// Keep existing behaviour — iteration markers are not
	// status transitions, so they stay out of the FSM.
}

// Verdict writes one verdict marker.
func (lc *VerifyLifecycle) Verdict(
	iteration int, verdict, findingsPath string,
) {
}

// IterationEnd closes the iteration_begin/end pairing.
func (lc *VerifyLifecycle) IterationEnd(iteration int, verdict string) {
}
