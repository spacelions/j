package testcases_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/coding-agents/codex"
)

// TestSPA94WatcherSilentWhenContextCancelled pins acceptance criteria
// AC4 for the cancellation path. When the orchestrator's context is
// cancelled (e.g. user hit ^C), WatchAndSaveActiveResumeID must
// unblock promptly, return "" without recording anything, and without
// printing transient noise to stderr.
func TestSPA94WatcherSilentWhenContextCancelled(t *testing.T) {
	var stderr bytes.Buffer
	recorder := &spa94Recorder{}
	capture := codingagents.ResumeCapture{
		TaskDir: t.TempDir(),
		Since:   time.Now(),
		Stderr:  &stderr,
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	done := make(chan string, 1)
	go func() {
		done <- codingagents.WatchAndSaveActiveResumeID(
			ctx, codex.New(), recorder, capture, os.Getpid(),
		)
	}()

	select {
	case got := <-done:
		if got != "" {
			t.Fatalf(
				"WatchAndSaveActiveResumeID = %q, want \"\" on "+
					"cancelled context", got,
			)
		}
	case <-time.After(2 * time.Second):
		t.Fatal(
			"watcher did not unblock within 2s of context cancel — " +
				"AC4 (silent capture) requires prompt teardown",
		)
	}
	if rec := recorder.snapshot(); rec != "" {
		t.Fatalf("recorder.id = %q, want \"\"", rec)
	}
	if stderr.Len() != 0 {
		t.Fatalf(
			"stderr = %q, want empty on cancel path",
			stderr.String(),
		)
	}
}
