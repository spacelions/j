package verify

import (
	"fmt"
	"io"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// agentLogFileName is the per-task file that captures stdout/stderr
// of a fire-and-forget headless cursor-agent child for `j verify`.
// It lives at `<cwd>/.j/tasks/<id>/agent.log`, matching the constant
// of the same name in the work / plan packages so all flows share a
// single filename.
const agentLogFileName = "agent.log"

// verifyOutcome enumerates the terminal results of runVerifyLoop.
// success → finalize as `completed` with DoneAt stamped; noRetries
// → finalize as `verify-done` (the loop exhausted the iteration cap
// without converging on PASS); errors are surfaced separately via
// the runErr argument so the lifecycle helper can pick the
// `help` status.
type verifyOutcome int

const (
	// outcomeSuccess means the verifier returned VERDICT: PASS at
	// some iteration; the task can be finalised as `completed`.
	outcomeSuccess verifyOutcome = iota
	// outcomeNoRetries means the loop exhausted MaxIterations
	// without a PASS verdict; the task ends as `verify-done`.
	outcomeNoRetries
)

// verifyLifecycle owns the begin/end task-log writes around a
// single `j verify` invocation. Mirrors workLifecycle in `j work`:
// the struct holds no bbolt handle — every task-log write goes
// through writeVerifyTaskWarn, which opens, writes, and closes
// within a single call so the bbolt file lock is never held across
// agent.Verify and a concurrent `j tasks` from another shell is not
// blocked.
type verifyLifecycle struct {
	stderr io.Writer
	task   store.Task
	closed bool
}

// beginVerifyTask flips an existing task row to `verifying`, stamps
// VerifyBeginAt, clears stale VerifyEndAt / DoneAt from a previous
// failed run, and records the latest tool/model and resume cursor
// for the verify phase. Plan-phase and work-phase fields are
// preserved.
func beginVerifyTask(opts Options, agent codingagents.Agent, model string, existing store.Task, verifyResumeChatID string) *verifyLifecycle {
	begin := time.Now().UTC()
	task := existing
	task.Status = store.StatusVerifying
	task.InvokedTool = agent.Name()
	task.InvokedModel = model
	task.VerifyResumeCursor = verifyResumeChatID
	task.VerifyBeginAt = &begin
	task.VerifyEndAt = nil
	task.DoneAt = nil
	return openLifecycle(opts, task)
}

// beginVerifyTaskResume is the resume-flow companion of
// beginVerifyTask. It diverges from beginVerifyTask in two places:
//
//  1. The existing VerifyResumeCursor is preserved verbatim
//     instead of being overwritten with a fresh
//     `Agent.NewResumeID` value (the whole point of resume is
//     reusing the cursor recorded on the task row).
//  2. The original VerifyBeginAt timestamp is preserved when set
//     so the task row keeps its first-run lineage; only
//     VerifyEndAt / DoneAt are cleared so finishVerify stamps
//     fresh values on the next finalize. Tool/model are kept
//     verbatim because resume never re-prompts the user for them.
func beginVerifyTaskResume(opts Options, existing store.Task) *verifyLifecycle {
	task := existing
	task.Status = store.StatusVerifying
	task.VerifyEndAt = nil
	task.DoneAt = nil
	if task.VerifyBeginAt == nil {
		begin := time.Now().UTC()
		task.VerifyBeginAt = &begin
	}
	return openLifecycle(opts, task)
}

// openLifecycle is the shared helper that best-effort writes the
// initial row and returns a verifyLifecycle suitable for
// finishVerify. The bbolt handle is opened, written to, and closed
// within writeVerifyTaskWarn so the file lock is not held across
// agent.Verify.
func openLifecycle(opts Options, task store.Task) *verifyLifecycle {
	lc := &verifyLifecycle{stderr: opts.Stderr, task: task}
	writeVerifyTaskWarn(opts.Stderr, task)
	return lc
}

// recordBackground stamps the spawned child's PID and the agent log
// path on the in-memory verify task row and re-persists it. The row
// stays at status `verifying` until the reaper in `j tasks`
// observes the child exited and finalises it. Mirrors the
// workLifecycle helper in `j work`.
func (lc *verifyLifecycle) recordBackground(pid int, logPath string) {
	if lc.closed {
		return
	}
	lc.closed = true
	lc.task.BackgroundPID = pid
	lc.task.AgentLogPath = logPath
	writeVerifyTaskWarn(lc.stderr, lc.task)
}

// finishVerify stamps verify_end_at, picks the terminal status from
// (outcome, runErr), and rewrites the task row.
//
//   - runErr != nil → status `help`, DoneAt unchanged.
//   - outcome == outcomeSuccess → status `completed`, DoneAt stamped.
//   - outcome == outcomeNoRetries → status `verify-done`, DoneAt unchanged.
//
// Calling finishVerify twice is a silent no-op via the closed flag.
func (lc *verifyLifecycle) finishVerify(outcome verifyOutcome, runErr error) {
	if lc.closed {
		return
	}
	lc.closed = true
	end := time.Now().UTC()
	lc.task.VerifyEndAt = &end
	switch {
	case runErr != nil:
		lc.task.Status = store.StatusHelp
	case outcome == outcomeSuccess:
		lc.task.Status = store.StatusCompleted
		done := time.Now().UTC()
		lc.task.DoneAt = &done
	default:
		lc.task.Status = store.StatusVerifyDone
	}
	writeVerifyTaskWarn(lc.stderr, lc.task)
}

// writeVerifyTaskWarn opens `<cwd>/.j/tasks/list.db`, writes task,
// and closes the store. Mirrors writeWorkTaskWarn in `j work`.
func writeVerifyTaskWarn(stderr io.Writer, task store.Task) {
	s, ok := openTaskLog(stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		fmt.Fprintf(stderr, "warning: tasks put: %v\n", err)
	}
}
