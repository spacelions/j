package codingagents

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ResumeIDCapturer is the optional, post-run companion to NewResumeID
// for backends whose CLI mints the session id only after the first
// turn writes to disk (deepseek-tui has no `--session-id`-style
// pre-run binding flag). The orchestrator type-asserts the chosen
// agent against this interface and, when satisfied, runs
// CaptureResumeID with the same workspace passed to Plan/Work/Verify
// and the phase's begin-at timestamp; the returned id is then
// persisted into the task row's *_resume_session field so a later
// resume run threads `--resume <id>` into the backend. Backends that
// mint the id pre-run (cursor / claude) intentionally do NOT
// implement this interface.
type ResumeIDCapturer interface {
	// CaptureResumeID resolves the session id minted by the most
	// recent run for (workspace, since). Implementations scan their
	// on-disk session store and return the newest entry whose
	// workspace matches and whose creation timestamp is >= since.
	// ("", nil) means "no matching session found"; ("", err) means
	// the scan itself failed and the caller should warn-and-continue.
	CaptureResumeID(
		ctx context.Context, workspace string, since time.Time,
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
	workspace string, since time.Time,
) (string, error) {
	capturer, ok := agent.(ResumeIDCapturer)
	if !ok {
		return "", nil
	}
	return capturer.CaptureResumeID(ctx, workspace, since)
}

// ResumeRecorder is the narrow lifecycle method
// CaptureAndRecordResume needs. Each per-phase Lifecycle in
// internal/lifecycle satisfies it via RecordResumeSession, so callers
// pass `lc` directly without an explicit adapter.
type ResumeRecorder interface {
	RecordResumeSession(id string)
}

// CaptureAndRecordResume is the shared post-run capture step for
// every plan / work / verify call site. It runs CaptureResumeID,
// stamps the result onto recorder, and surfaces scan failures via
// stderr without aborting the run. The captured id is returned so
// callers (e.g. the verifier loop) that need to thread the id into a
// later iteration can pick it up; an empty string signals "no match"
// and is the expected outcome for cursor/claude.
func CaptureAndRecordResume(
	ctx context.Context, agent Agent, recorder ResumeRecorder,
	workspace string, since time.Time, stderr io.Writer,
) string {
	id, err := CaptureResumeID(ctx, agent, workspace, since)
	if err != nil {
		fmt.Fprintf(stderr, "J: %v\n", err)
		return ""
	}
	recorder.RecordResumeSession(id)
	return id
}
