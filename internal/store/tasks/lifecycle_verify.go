package tasks

import (
	"io"
	"time"
)

// VerifyOutcome enumerates the terminal results of `j verify`'s
// fix-loop. VerifyOutcomeSuccess means the verifier returned VERDICT:
// PASS at some iteration; the task can be finalised as `completed`.
// VerifyOutcomeNoRetries means the loop exhausted MaxIterations
// without a PASS verdict; the task ends as `verify-done`. Errors are
// surfaced separately via the runErr argument so VerifyLifecycle.Finish
// can pick the `help` status.
type VerifyOutcome int

const (
	// VerifyOutcomeSuccess: verifier returned PASS; finalise as
	// `completed` with DoneAt stamped.
	VerifyOutcomeSuccess VerifyOutcome = iota
	// VerifyOutcomeNoRetries: loop exhausted without a PASS; finalise
	// as `verify-done`.
	VerifyOutcomeNoRetries
)

// VerifyLifecycle owns the begin/end task-log writes around a single
// `j verify` invocation. Mirrors WorkLifecycle: the struct holds no
// bbolt handle — every task-log write goes through PersistWarn so
// the bbolt file lock is never held across agent.Verify and a
// concurrent `j tasks` from another shell is not blocked.
type VerifyLifecycle struct {
	stderr io.Writer
	task   Task
	closed bool
}

// BeginVerify flips an existing task row to `verifying`, stamps
// VerifyBeginAt, clears stale VerifyEndAt / DoneAt from a previous
// failed run, and records the latest tool/model and resume cursor
// for the verify phase. Plan-phase and work-phase fields are
// preserved.
func (t Task) BeginVerify(stderr io.Writer, agentName, model, resumeID string) *VerifyLifecycle {
	begin := time.Now().UTC()
	task := t
	task.Status = StatusVerifying
	task.InvokedTool = agentName
	task.InvokedModel = model
	task.VerifyResumeCursor = resumeID
	task.VerifyBeginAt = &begin
	task.VerifyEndAt = nil
	task.DoneAt = nil
	return openVerifyLifecycle(stderr, task)
}

// BeginVerifyResume is the resume-flow companion of BeginVerify. It
// diverges from BeginVerify in two places:
//
//  1. The existing VerifyResumeCursor is preserved verbatim instead
//     of being overwritten with a fresh `Agent.NewResumeID` value.
//  2. The original VerifyBeginAt timestamp is preserved when set so
//     the task row keeps its first-run lineage; only VerifyEndAt /
//     DoneAt are cleared so Finish stamps fresh values on the next
//     finalize. Tool/model are kept verbatim because resume never
//     re-prompts the user for them.
func (t Task) BeginVerifyResume(stderr io.Writer) *VerifyLifecycle {
	task := t
	task.Status = StatusVerifying
	task.VerifyEndAt = nil
	task.DoneAt = nil
	if task.VerifyBeginAt == nil {
		begin := time.Now().UTC()
		task.VerifyBeginAt = &begin
	}
	return openVerifyLifecycle(stderr, task)
}

// openVerifyLifecycle is the shared helper that best-effort writes
// the initial row and returns a VerifyLifecycle suitable for Finish.
func openVerifyLifecycle(stderr io.Writer, task Task) *VerifyLifecycle {
	lc := &VerifyLifecycle{stderr: stderr, task: task}
	PersistWarn(stderr, task)
	emitPhaseBegin(stderr, "verify", task)
	return lc
}

// RecordBackground stamps the spawned child's PID and the agent log
// path on the in-memory verify task row and re-persists it. The row
// stays at status `verifying` until the reaper in `j tasks` observes
// the child exited and finalises it.
func (lc *VerifyLifecycle) RecordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	PersistWarn(lc.stderr, lc.task)
}

// Finish stamps verify_end_at, picks the terminal status from
// (outcome, runErr), and rewrites the task row.
//
//   - runErr != nil → status `help`, DoneAt unchanged.
//   - outcome == VerifyOutcomeSuccess → status `completed`, DoneAt
//     stamped.
//   - outcome == VerifyOutcomeNoRetries → status `verify-done`,
//     DoneAt unchanged.
//
// Calling Finish twice is a silent no-op via the closed flag.
func (lc *VerifyLifecycle) Finish(outcome VerifyOutcome, runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.VerifyEndAt = &end
	markerOutcome := "fail"
	switch {
	case runErr != nil:
		lc.task.Status = StatusHelp
		markerOutcome = "help"
	case outcome == VerifyOutcomeSuccess:
		lc.task.Status = StatusCompleted
		done := time.Now().UTC()
		lc.task.DoneAt = &done
		markerOutcome = "pass"
	default:
		lc.task.Status = StatusVerifyDone
	}
	PersistWarn(lc.stderr, lc.task)
	emitPhaseEnd(lc.stderr, "verify", lc.task.VerifyBeginAt, lc.task, markerOutcome)
}
