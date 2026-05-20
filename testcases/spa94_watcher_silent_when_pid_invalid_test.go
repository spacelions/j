package testcases_test

import (
	"bytes"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
)

// TestSPA94WatcherSilentWhenPIDInvalid pins acceptance criteria AC4
// (silent capture) for the degenerate "no pid" case. The orchestrator
// can hand the watcher pid <= 0 when the backend's Plan / Work / Verify
// call returned inline rather than forking a subprocess. The watcher
// must short-circuit to "" without crashing, without touching the
// recorder, and without spamming stderr.
func TestSPA94WatcherSilentWhenPIDInvalid(t *testing.T) {
	var stderr bytes.Buffer
	recorder := &spa94Recorder{}
	capture := codingagents.ResumeCapture{
		TaskDir: t.TempDir(),
		Since:   time.Now(),
		Stderr:  &stderr,
	}
	got := codingagents.WatchAndSaveActiveResumeID(
		t.Context(), codex.New(), recorder, capture, 0,
	)
	if got != "" {
		t.Fatalf(
			"WatchAndSaveActiveResumeID = %q, want \"\" with pid=0",
			got,
		)
	}
	if rec := recorder.snapshot(); rec != "" {
		t.Fatalf("recorder.id = %q, want \"\"", rec)
	}
	if stderr.Len() != 0 {
		t.Fatalf(
			"stderr = %q, want empty — watcher must not spam on "+
				"the no-pid path (AC4: silent capture)",
			stderr.String(),
		)
	}
}
