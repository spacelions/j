package testcases_test

import (
	"bytes"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
)

// TestSPA94WatcherSilentWhenWorkerPIDDead pins acceptance criteria
// AC4 plus the bounded-liveness contract in the plan. When the worker
// subprocess has already exited before the watcher ever sees an
// fsnotify event (e.g. codex crashed before writing session_meta),
// the watcher's pid-liveness ticker must end the loop within a few
// hundred milliseconds, return "", and leave the recorder untouched.
//
// A PID that the kernel almost certainly does not own (999999) is the
// closest black-box stand-in for a dead worker pid that the test can
// drive without forking and killing a real child.
func TestSPA94WatcherSilentWhenWorkerPIDDead(t *testing.T) {
	var stderr bytes.Buffer
	recorder := &spa94Recorder{}
	capture := codingagents.ResumeCapture{
		TaskDir: t.TempDir(),
		Since:   time.Now(),
		Stderr:  &stderr,
	}

	done := make(chan string, 1)
	go func() {
		done <- codingagents.WatchAndSaveActiveResumeID(
			t.Context(), codex.New(), recorder, capture, 999999,
		)
	}()

	select {
	case got := <-done:
		if got != "" {
			t.Fatalf(
				"WatchAndSaveActiveResumeID = %q, want \"\" when "+
					"the worker pid is gone", got,
			)
		}
	case <-time.After(3 * time.Second):
		t.Fatal(
			"watcher did not unblock within 3s of a dead worker " +
				"pid — the bounded liveness ticker is missing",
		)
	}
	if rec := recorder.snapshot(); rec != "" {
		t.Fatalf("recorder.id = %q, want \"\"", rec)
	}
	if stderr.Len() != 0 {
		t.Fatalf(
			"stderr = %q, want empty on dead-pid path",
			stderr.String(),
		)
	}
}
