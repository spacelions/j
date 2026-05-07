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
	VerifyOutcomeSuccess VerifyOutcome = iota
	VerifyOutcomeNoRetries
)

// VerifyLifecycle owns the begin/end task-log writes around a
// single `j verify` invocation.
type VerifyLifecycle struct {
	stderr        io.Writer
	agentLogPath  string
	task          tasks.Task
	maxIterations int
	closed        bool
}

// BeginVerifyRestart flips an existing task row to `verifying` for
// the re-verify / first-run flow: tool/model/resume cursor are
// refreshed and stale verify timestamps cleared. Mirrors the
// BeginPlanRestart / BeginWorkRestart shape so the restart vs resume
// vocabulary is uniform across the lifecycle helpers.
func BeginVerifyRestart(t tasks.Task, stderr io.Writer, agentName, model,
	resumeID, agentLogPath string,
) *VerifyLifecycle {
	task := t
	task.VerifyTool = agentName
	task.VerifyModel = model
	task.VerifyResumeSession = resumeID
	task.VerifyBeginAt = time.Now().UTC()
	task.VerifyEndAt = time.Time{}
	task.DoneAt = time.Time{}
	return openVerifyLifecycle(stderr, task, agentLogPath,
		tasks.EventVerifyBegin, "verify begin")
}

// BeginVerifyResume is the resume-flow companion of BeginVerifyRestart.
func BeginVerifyResume(t tasks.Task, stderr io.Writer,
	agentLogPath string,
) *VerifyLifecycle {
	task := t
	task.VerifyEndAt = time.Time{}
	task.DoneAt = time.Time{}
	if task.VerifyBeginAt.IsZero() {
		task.VerifyBeginAt = time.Now().UTC()
	}
	return openVerifyLifecycle(stderr, task, agentLogPath,
		tasks.EventVerifyResume, "verify resume")
}

func openVerifyLifecycle(stderr io.Writer, task tasks.Task,
	agentLogPath string, ev tasks.Event, panicTag string,
) *VerifyLifecycle {
	task.AgentLogPath = agentLogPath
	if _, err := tasks.ApplyAndPersistWarn(
		stderr, &task, ev); err != nil {
		panic(panicTag + ": " + err.Error())
	}
	return &VerifyLifecycle{
		stderr:       stderr,
		agentLogPath: agentLogPath,
		task:         task,
	}
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
	if _, err := tasks.ApplyAndPersistWarn(
		lc.stderr, &lc.task, ev); err != nil {
		panic("verify finish: " + err.Error())
	}
}

// IterationBegin records the iteration cap so a later FAIL Verdict
// can render the per-iteration Linear comment with an "N/M" header.
func (lc *VerifyLifecycle) IterationBegin(iteration, max int) {
	lc.maxIterations = max
}

// Verdict mirrors a per-iteration FAIL to the linked Linear issue
// (verifier_findings.md prefixed with the iteration header). PASS
// verdicts are skipped — the terminal hook handles the success
// comment.
func (lc *VerifyLifecycle) Verdict(
	iteration int, verdict, findingsPath string,
) {
	if verdict != "FAIL" {
		return
	}
	PushVerifyIterationFinding(
		lc.stderr, lc.task, iteration, lc.maxIterations,
	)
}

// IterationEnd closes the iteration_begin/end pairing.
func (lc *VerifyLifecycle) IterationEnd(iteration int, verdict string) {
}
