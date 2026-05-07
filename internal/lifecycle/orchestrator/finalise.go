package orchestrator

import (
	"io"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store/tasks"
)

// finaliseVerifyFailIfStuck flips a row still pinned at `verifying`
// to `failed` after the orchestrator's SequentialAgent drains.
// Best-effort: any read / write error surfaces as a single warning
// on stderr and the helper returns.
func finaliseVerifyFailIfStuck(stderr io.Writer, taskID string) {
	s, err := tasks.OpenDefault()
	if err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks dir: %v", err)
		return
	}
	defer func() { _ = s.Close() }()
	t, err := s.GetTask(taskID)
	if err != nil {
		return
	}
	if t.Status != tasks.StatusVerifying {
		return
	}
	t.Status = tasks.StatusFailed
	if err := s.PutTask(t); err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks put: %v", err)
	}
}
