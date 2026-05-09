package testcases_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// safeBuf is a goroutine-safe bytes.Buffer the streaming acceptance
// test polls while the cobra command writes through `tail -f`.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// TestTasksLogs_RendersAgentLog pins the acceptance bullet:
// `j tasks logs --from-task <id>` follows the resolved task's
// agent.log via `tail -f` and the rendered bytes appear on stdout.
// The test cancels the cobra context once the seed substring shows
// up to keep the streaming command from hanging the suite.
func TestTasksLogs_RendersAgentLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX tail")
	}
	if _, err := exec.LookPath("tail"); err != nil {
		t.Skip("tail not on PATH")
	}
	t.Chdir(t.TempDir())
	testutil.Init(t)

	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatalf("OpenDefault: %v", err)
	}
	if err := s.PutTask(tasks.Task{
		ID:        "id-render",
		Status:    tasks.StatusPlanDone,
		PlanTool:  "cursor",
		PlanModel: "sonnet-4",
		Summary:   "logs render via viewer",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("PutTask: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	dir, err := tasks.EnsureDir("id-render")
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	body := "agentlog: rendered via viewer\n"
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
	cmd.SetArgs([]string{"logs", "--from-task", "id-render"})

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	want := "rendered via viewer"
	for !strings.Contains(stdout.String(), want) {
		select {
		case <-deadline.Done():
			cancel()
			<-done
			t.Fatalf("timed out; stdout = %q", stdout.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf(
			"stdout = %q, want substring `rendered via viewer`",
			stdout.String(),
		)
	}
}
