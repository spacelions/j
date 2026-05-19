package testcases_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/claude"
)

// TestSPA94WatcherNoopForClaudeBackend pins acceptance criteria AC7:
// claude (and cursor by extension) mint their resume id pre-run via
// NewResumeID, so they intentionally do not implement
// ResumeIDCapturer. WatchAndSaveActiveResumeID must short-circuit to
// "" for these backends without invoking the recorder, so the post-
// fsnotify watcher path does not erase the pre-run id or wedge the
// orchestrator waiting on a watcher event that will never come.
func TestSPA94WatcherNoopForClaudeBackend(t *testing.T) {
	recorder := &spa94Recorder{}
	capture := codingagents.ResumeCapture{
		TaskDir: t.TempDir(),
		Since:   time.Now(),
		Stderr:  &bytes.Buffer{},
	}
	got := codingagents.WatchAndSaveActiveResumeID(
		t.Context(), claude.New(), recorder, capture, os.Getpid(),
	)
	if got != "" {
		t.Fatalf(
			"WatchAndSaveActiveResumeID = %q, want \"\" "+
				"(claude does not implement ResumeIDCapturer)", got,
		)
	}
	if rec := recorder.snapshot(); rec != "" {
		t.Fatalf(
			"recorder.RecordResumeSession = %q, want \"\" "+
				"(no-op backend must not touch the recorder)", rec,
		)
	}
}
