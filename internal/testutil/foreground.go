package testutil

import (
	"context"
	"sync/atomic"
	"time"
)

// ForegroundCapture is an embeddable test double for the
// ResumeIDCapturer-plus-blocking-phase shape the SPA-103 foreground
// tests need in the planner, worker, and verifier packages. Embed it
// by value into a stub agent: the zero value is ready to use (nil
// channels make Block a no-op), CaptureResumeID/SetCaptureID/Block are
// promoted onto the stub, and the stubbed phase method calls Block to
// park until the test releases it.
//
// It contains atomics, so embed it and use the stub through a pointer
// (never copy the stub) — the same constraint the hand-written stubs
// it replaces already obeyed.
type ForegroundCapture struct {
	id atomic.Pointer[string]
	// Calls counts CaptureResumeID invocations.
	Calls atomic.Int32
	// Release, when non-nil, blocks Block until the channel closes so
	// a test can assert mid-run state before letting the phase return.
	Release chan struct{}
	// Started, when non-nil, is closed by Block so a test can wait
	// until the stubbed phase is actually executing.
	Started chan struct{}
}

// SetCaptureID sets the id CaptureResumeID returns. Safe to call
// concurrently with the watcher goroutine that drives CaptureResumeID.
func (f *ForegroundCapture) SetCaptureID(id string) {
	f.id.Store(&id)
}

// CaptureResumeID records the call and returns the id set via
// SetCaptureID, or ("", nil) before one is set. It satisfies
// codingagents.ResumeIDCapturer once promoted onto the embedding stub.
func (f *ForegroundCapture) CaptureResumeID(
	context.Context, string, time.Time,
) (string, error) {
	f.Calls.Add(1)
	if p := f.id.Load(); p != nil {
		return *p, nil
	}
	return "", nil
}

// Block signals Started and then parks on Release. Call it at the top
// of the stubbed phase method (Plan / Work / Verify) so a foreground
// test can observe the task row while the phase is still running.
func (f *ForegroundCapture) Block() {
	if f.Started != nil {
		close(f.Started)
	}
	if f.Release != nil {
		<-f.Release
	}
}
