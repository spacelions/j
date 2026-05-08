package testcases_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/lifecycle"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestCase_PRURL_MarkerLineIncludesURL pins acceptance criterion #2:
// when WorkLifecycle.Finish persists EventWorkDone with a non-empty
// PullRequestURL, the markers hook (registered via lifecycle.Init)
// must append a `work done — pull request: <url>` line to the
// agent.log. The visible side-effect is the log line — that's what
// users / Linear inspectors will read.
func TestCase_PRURL_MarkerLineIncludesURL(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	tasks.ResetHooksForTest()
	t.Cleanup(tasks.ResetHooksForTest)
	lifecycle.Init()

	logPath := filepath.Join(t.TempDir(), "agent.log")
	prURL := "https://github.com/spacelions/j/pull/9001"
	if err := os.WriteFile(logPath,
		[]byte("opened "+prURL+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lc := lifecycle.NewWorkTask(io.Discard, "cursor", "sonnet-4",
		tasks.NewTaskID(), "/tmp/x.plan.md", "", "body", "",
		logPath)
	lc.Finish(nil)

	body := readFileForMarker(t, logPath)
	want := "work done — pull request: " + prURL
	if !strings.Contains(body, want) {
		t.Fatalf("agent.log missing marker line.\nwant substring: %q\ngot:\n%s",
			want, body)
	}
}

func readFileForMarker(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
