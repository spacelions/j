package tasks

import (
	"io"
	"time"
)

// VerifyOutcome enumerates the terminal results of `j verify`'s
// fix-loop. VerifyOutcomeSuccess means the verifier returned VERDICT:
// PASS at some iteration; the task can be finalised as `completed`.
// VerifyOutcomeNoRetries means the loop exhausted MaxIterations
// without a PASS verdict; the task ends as `failed`. Errors are
// surfaced separately via the runErr argument so VerifyLifecycle.Finish
// can pick the `help` status.
type VerifyOutcome int

const (
	// VerifyOutcomeSuccess: verifier returned PASS; finalise as
	// `completed` with DoneAt stamped.
	VerifyOutcomeSuccess VerifyOutcome = iota
	// VerifyOutcomeNoRetries: loop exhausted without a PASS; finalise
	// as `failed`.
	VerifyOutcomeNoRetries
)

// VerifyLifecycle owns the begin/end task-log writes around a single
// `j verify` invocation. Mirrors WorkLifecycle: the struct holds no
// bbolt handle — every task-log write goes through PersistWarn so
// the bbolt file lock is never held across agent.Verify and a
// concurrent `j tasks` from another shell is not blocked.
//
// agentLogPath is the per-task `agent.log` destination for phase
// markers; empty string disables marker emission (test paths).
type VerifyLifecycle struct {
	stderr       io.Writer
	agentLogPath string
	task         Task
	closed       bool
}

// BeginVerify flips an existing task row to `verifying`, stamps
// VerifyBeginAt, clears stale VerifyEndAt / DoneAt from a previous
// failed run, and records the latest tool/model and resume session
// for the verify phase. Plan-phase and work-phase fields are
// preserved.
func (t Task) BeginVerify(stderr io.Writer, agentName, model, resumeID, agentLogPath string) *VerifyLifecycle {
	task := t
	task.Status = StatusVerifying
	task.VerifyTool = agentName
	task.VerifyModel = model
	task.VerifyResumeSession = resumeID
	task.VerifyBeginAt = time.Now().UTC()
	task.VerifyEndAt = time.Time{}
	task.DoneAt = time.Time{}
	return openVerifyLifecycle(stderr, task, agentLogPath)
}

// BeginVerifyResume is the resume-flow companion of BeginVerify. It
// diverges from BeginVerify in two places:
//
//  1. The existing VerifyResumeSession is preserved verbatim instead
//     of being overwritten with a fresh `Agent.NewResumeID` value.
//  2. The original VerifyBeginAt timestamp is preserved when set so
//     the task row keeps its first-run lineage; only VerifyEndAt /
//     DoneAt are cleared so Finish stamps fresh values on the next
//     finalize. Tool/model are kept verbatim because resume never
//     re-prompts the user for them.
func (t Task) BeginVerifyResume(stderr io.Writer, agentLogPath string) *VerifyLifecycle {
	task := t
	task.Status = StatusVerifying
	task.VerifyEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.VerifyBeginAt.IsZero() {
		task.VerifyBeginAt = time.Now().UTC()
	}
	return openVerifyLifecycle(stderr, task, agentLogPath)
}

// openVerifyLifecycle is the shared helper that best-effort writes
// the initial row and returns a VerifyLifecycle suitable for Finish.
func openVerifyLifecycle(stderr io.Writer, task Task, agentLogPath string) *VerifyLifecycle {
	lc := &VerifyLifecycle{stderr: stderr, agentLogPath: agentLogPath, task: task}
	PersistWarn(stderr, task)
	emitPhaseBegin(agentLogPath, "verify", task)
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
//   - outcome == VerifyOutcomeNoRetries → status `failed`,
//     DoneAt unchanged.
//
// Calling Finish twice is a silent no-op via the closed flag.
func (lc *VerifyLifecycle) Finish(outcome VerifyOutcome, runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.VerifyEndAt = time.Now().UTC()
	markerOutcome := "fail"
	switch {
	case runErr != nil:
		lc.task.Status = StatusHelp
		markerOutcome = "help"
	case outcome == VerifyOutcomeSuccess:
		lc.task.Status = StatusCompleted
		lc.task.DoneAt = time.Now().UTC()
		markerOutcome = "pass"
	default:
		lc.task.Status = StatusFailed
	}
	PersistWarn(lc.stderr, lc.task)
	emitPhaseEnd(lc.agentLogPath, "verify", lc.task.VerifyBeginAt, lc.task, markerOutcome)
}

// IterationBegin writes one verify_iteration_begin marker to the
// per-task agent.log at the start of each fix-loop turn. An empty
// agentLogPath (test paths) is a silent no-op. Does not interact with
// the closed flag — iteration markers fire many times mid-run.
func (lc *VerifyLifecycle) IterationBegin(iteration, max int) {
	emitVerifyIterationBegin(lc.agentLogPath, lc.task.ID, iteration, max)
}

// Verdict writes one verdict marker carrying the parsed PASS/FAIL
// plus the findings path so a tailer can correlate without re-reading
// verifier_findings.md.
func (lc *VerifyLifecycle) Verdict(iteration int, verdict, findingsPath string) {
	emitVerdict(lc.agentLogPath, lc.task.ID, iteration, verdict, findingsPath)
}

// IterationEnd closes the iteration_begin/end pairing per loop turn.
func (lc *VerifyLifecycle) IterationEnd(iteration int, verdict string) {
	emitVerifyIterationEnd(lc.agentLogPath, lc.task.ID, iteration, verdict)
}
