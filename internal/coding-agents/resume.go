package codingagents

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spacelions/j/internal/util/run"
)

const (
	activeResumeCaptureInterval = 100 * time.Millisecond
	activeResumeCaptureTimeout  = 2 * time.Second
)

// ResumeIDCapturer is the optional, post-run companion to NewResumeID
// for backends whose CLI mints the session id only after the first
// turn writes to disk (deepseek-tui has no `--session-id`-style
// pre-run binding flag). The orchestrator type-asserts the chosen
// agent against this interface and, when satisfied, runs
// CaptureResumeID with the per-task directory and the phase's
// begin-at timestamp; the returned id is then
// persisted into the task row's *_resume_session field so a later
// resume run threads `--resume <id>` into the backend. Backends that
// mint the id pre-run (cursor / claude) intentionally do NOT
// implement this interface.
type ResumeIDCapturer interface {
	// CaptureResumeID resolves the session id minted by the most
	// recent run for (taskDir, since). Implementations scan their
	// task-scoped on-disk session store and return the newest entry
	// whose creation timestamp is >= since.
	// ("", nil) means "no matching session found"; ("", err) means
	// the scan itself failed and the caller should warn-and-continue.
	CaptureResumeID(
		ctx context.Context, taskDir string, since time.Time,
	) (string, error)
}

// CaptureResumeID is the type-assertion-aware free helper the
// orchestrator/lifecycle wiring uses to ask any Agent for a post-run
// session id. Backends that do not satisfy ResumeIDCapturer return
// ("", nil), which lets call sites do an unconditional best-effort
// capture without sniffing agent.Name(). A non-nil scan error is
// surfaced to the caller so it can warn (and continue) rather than
// silently dropping the failure.
func CaptureResumeID(
	ctx context.Context, agent Agent,
	taskDir string, since time.Time,
) (string, error) {
	capturer, ok := agent.(ResumeIDCapturer)
	if !ok {
		return "", nil
	}
	return capturer.CaptureResumeID(ctx, taskDir, since)
}

// ResumeRecorder is the narrow lifecycle method the save helpers need.
// Each per-phase Lifecycle in internal/lifecycle satisfies it via
// RecordResumeSession, so callers pass `lc` directly.
type ResumeRecorder interface {
	RecordResumeSession(id string)
}

// ResumeCapture groups the filesystem and timing data needed to
// discover a post-run resume id.
type ResumeCapture struct {
	TaskDir string
	Since   time.Time
	Stderr  io.Writer
}

type ResumeProcess struct {
	PID      int
	Wait     bool
	ResumeID string
}

// CaptureAndSaveResumeID is the shared post-run capture step for
// every plan / work / verify call site. It runs CaptureResumeID,
// stamps the result onto recorder, and surfaces scan failures via
// stderr without aborting the run. The captured id is returned so
// callers (e.g. the verifier loop) that need to thread the id into a
// later iteration can pick it up; an empty string signals "no match"
// and is the expected outcome for cursor/claude.
func CaptureAndSaveResumeID(
	ctx context.Context, agent Agent, recorder ResumeRecorder,
	capture ResumeCapture,
) string {
	id, err := CaptureResumeID(
		ctx, agent, capture.TaskDir, capture.Since,
	)
	if err != nil {
		fmt.Fprintf(capture.Stderr, "J: %v\n", err)
		return ""
	}
	recorder.RecordResumeSession(id)
	return id
}

// CaptureAndSaveActiveResumeID polls a running backend until its
// post-start resume id appears, the process exits, the context is
// cancelled, or the bounded capture window expires.
func CaptureAndSaveActiveResumeID(
	ctx context.Context,
	agent Agent,
	recorder ResumeRecorder,
	capture ResumeCapture,
	pid int,
) string {
	capturer, ok := agent.(ResumeIDCapturer)
	if !ok || pid <= 0 {
		return ""
	}
	if id, done := captureAndSaveOnce(ctx, capturer, recorder, capture); done {
		return id
	}
	timeout := time.NewTimer(activeResumeCaptureTimeout)
	defer timeout.Stop()
	timer := time.NewTimer(activeResumeCaptureInterval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ""
		case <-timeout.C:
			return ""
		case <-timer.C:
			id, done := captureAndSaveOnce(
				ctx, capturer, recorder, capture,
			)
			if done {
				return id
			}
			if !run.IsAlive(pid) {
				return ""
			}
			timer.Reset(activeResumeCaptureInterval)
		}
	}
}

func CaptureAndSaveProcessResumeID(
	ctx context.Context,
	agent Agent,
	recorder ResumeRecorder,
	capture ResumeCapture,
	proc ResumeProcess,
) (string, error) {
	resumeID := proc.ResumeID
	if proc.PID > 0 && resumeID == "" {
		resumeID = CaptureAndSaveActiveResumeID(
			ctx, agent, recorder, capture, proc.PID,
		)
	}
	if proc.PID > 0 && proc.Wait {
		if err := run.WaitForExit(ctx, proc.PID); err != nil {
			return resumeID, err
		}
	}
	if proc.Wait && resumeID == "" {
		resumeID = CaptureAndSaveResumeID(
			ctx, agent, recorder, capture,
		)
	}
	return resumeID, nil
}

func WaitForResumeProcess(ctx context.Context, pid int) error {
	return run.WaitForExit(ctx, pid)
}

func captureAndSaveOnce(
	ctx context.Context,
	capturer ResumeIDCapturer,
	recorder ResumeRecorder,
	capture ResumeCapture,
) (string, bool) {
	id, err := capturer.CaptureResumeID(
		ctx, capture.TaskDir, capture.Since,
	)
	if err != nil {
		fmt.Fprintf(capture.Stderr, "J: %v\n", err)
		return "", true
	}
	if id == "" {
		return "", false
	}
	recorder.RecordResumeSession(id)
	return id, true
}
