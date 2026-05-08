package testcases_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
)

// fireStateSyncTransitionWithLog dispatches a synthetic transition
// with AgentLogPath populated so the needs-clarification branch can
// locate the task directory holding clarification.md.
func fireStateSyncTransitionWithLog(
	taskID, linearIssue, agentLogPath string,
	from, to tasks.TaskStatus, ev tasks.Event,
) {
	tasks.Notify(
		tasks.Transition{From: from, Event: ev, To: to},
		tasks.Task{
			ID: taskID, Status: to, LinearIssue: linearIssue,
			AgentLogPath: agentLogPath,
		},
	)
}

// writeClarificationDir writes clarification.md with the given body
// into a fresh temp dir and returns the agent.log path inside that
// dir, mirroring the reaper layout `<tasksDir>/<id>/`.
func writeClarificationDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "clarification.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write clarification.md: %v", err)
	}
	return filepath.Join(dir, "agent.log")
}

// agentLogPathOnlyDir returns an agent.log path inside a fresh temp
// dir without creating clarification.md, so the file-missing branch
// is exercised.
func agentLogPathOnlyDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "agent.log")
}
