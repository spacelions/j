package lifecycle

import (
	"io"
	"os"
	"path/filepath"
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

// RecordResumeSession stamps id onto the in-memory verify task row's
// VerifyResumeSession field and re-persists. See PlanLifecycle's
// equivalent for the post-run-capture rationale.
func (lc *VerifyLifecycle) RecordResumeSession(id string) {
	if id == "" {
		return
	}
	lc.task.VerifyResumeSession = id
	tasks.PersistWarn(lc.stderr, lc.task)
}

// RecordAgentLog stamps the per-task agent.log path on the verify
// task row. See PlanLifecycle.RecordAgentLog for the SPA-72 rationale.
func (lc *VerifyLifecycle) RecordAgentLog(logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.AgentLogPath = logPath
	tasks.PersistWarn(lc.stderr, lc.task)
}

// Finish stamps verify_end_at and picks the terminal status from
// (outcome, runErr, clarification.md presence). runErr wins, then
// clarification.md (the resume agent re-raised the question), then
// the PASS / FAIL outcome — mirroring the planner / worker matrix
// so AC4's "task goes back to needs-clarification for another
// round" symmetry holds across all three phases.
func (lc *VerifyLifecycle) Finish(outcome VerifyOutcome, runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.VerifyEndAt = time.Now().UTC()

	ev := lc.pickFinishEvent(outcome, runErr)
	if _, err := tasks.ApplyAndPersistWarn(
		lc.stderr, &lc.task, ev); err != nil {
		panic("verify finish: " + err.Error())
	}
}

// pickFinishEvent decides which event drives the verify-finish
// transition. Error path takes precedence over the clarification
// check; clarification.md presence wins over the PASS/FAIL outcome
// so a re-raised question never silently lands at completed/failed.
// Mirrors PlanLifecycle.pickFinishEvent and
// WorkLifecycle.pickFinishEvent.
func (lc *VerifyLifecycle) pickFinishEvent(
	outcome VerifyOutcome, runErr error,
) tasks.Event {
	if runErr != nil {
		return tasks.EventVerifyError
	}
	if lc.clarificationPresent() {
		return tasks.EventVerifyNeedsClarification
	}
	if outcome == VerifyOutcomeSuccess {
		return tasks.EventVerifyPass
	}
	return tasks.EventVerifyFail
}

// clarificationPresent reports whether the verifier left
// `<tasksDir>/<task.ID>/clarification.md` on disk. A missing tasks
// dir or any other stat error counts as "absent" so the historical
// PASS/FAIL matrix stays the default.
func (lc *VerifyLifecycle) clarificationPresent() bool {
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		return false
	}
	path := filepath.Join(
		tasksDir, lc.task.ID, tasks.ClarificationFileName)
	_, err = os.Stat(path)
	return err == nil
}

// IterationBegin records the iteration cap so a later FAIL Verdict
// can render the per-iteration Linear comment with an "N/M" header.
func (lc *VerifyLifecycle) IterationBegin(iteration, iterMax int) {
	lc.maxIterations = iterMax
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
