package testcases_test

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestTasksLogs_ContextCancelExitsZero pins the acceptance bullet:
// "Interrupting the command (SIGINT or context cancel) terminates
// the follower cleanly and exits 0."
//
// The test runs `j tasks logs --from-task <id>` with a cancellable
// context, waits for the seeded content to appear on stdout, cancels
// the context (simulating Ctrl+C), and asserts the command returns
// nil (exit code 0).
func TestTasksLogs_ContextCancelExitsZero(t *testing.T) {
	if _, err := exec.LookPath("tail"); err != nil {
		t.Skip("tail not on PATH")
	}
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s := tasks.OpenDefault()
	if err := s.PutTask(tasks.Task{
		ID:        "id-cancel",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "cancel test",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	dir, err := tasks.EnsureDir("id-cancel")
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	body := "pre-existing content\n"
	if err := os.WriteFile(
		filepath.Join(dir, tasks.AgentLogFileName),
		[]byte(body), 0o644,
	); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	deadline, cancelDeadline := context.WithTimeout(
		t.Context(), 5*time.Second,
	)
	defer cancelDeadline()
	ctx, cancel := context.WithCancel(deadline)
	defer cancel()

	stdout := &safeBuf{}
	cmd := clitasks.New()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(stdout)
	cmd.SetErr(io.Discard)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"logs", "--from-task", "id-cancel"})

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	// Poll until the pre-existing content appears.
	want := "pre-existing content"
	for !strings.Contains(stdout.String(), want) {
		select {
		case <-deadline.Done():
			cancel()
			<-done
			t.Fatalf("timed out waiting for content; stdout = %q",
				stdout.String())
		case <-time.After(20 * time.Millisecond):
		}
	}

	// Cancel the context (simulate Ctrl+C).
	cancel()

	// The command must return nil (exit 0) after cancel, per the
	// acceptance criterion: "exits 0 (matching POSIX tail -f
	// interrupt semantics for normal CLI usage)."
	if err := <-done; err != nil {
		t.Fatalf("Execute after cancel: %v, want nil", err)
	}
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf(
			"stdout = %q, want substring %q",
			stdout.String(), want,
		)
	}
}

// safeBuf is defined in tasks_logs_renders_agent_log_test.go;
// both files share the testcases_test package.
