package testcases_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/util/run"
)

// TestSpawn_ChildExitMarker_AppendsHumanReadable drives the SPA-28
// fix end-to-end through the `internal/util/run` public surface:
// `Spawn` must append a single human-readable `child exit` marker to
// the per-task log when the child reaps. No `>>> J ` sentinel and no
// JSON payload may appear on disk.
func TestSpawn_ChildExitMarker_AppendsHumanReadable(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "agent.log")
	pid, err := run.Spawn(
		t.Context(), logPath, "sh", "-c", "exit 0",
	)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(logPath)
		body := string(data)
		if strings.Contains(body, "child exit") {
			if strings.Contains(body, ">>> J ") {
				t.Fatalf("legacy sentinel leaked into %q", body)
			}
			if !strings.Contains(body, "exit_code=0") {
				t.Fatalf("missing exit_code=0 marker: %q", body)
			}
			pidNeedle := fmt.Sprintf("pid=%d", pid)
			if !strings.Contains(body, pidNeedle) {
				t.Fatalf("missing pid %d marker: %q", pid, body)
			}
			if !strings.Contains(body, "name=sh") {
				t.Fatalf("missing name=sh marker: %q", body)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for child exit marker: %q", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
