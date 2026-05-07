package orchestrator

import (
	"io"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store/tasks"
)

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
	newStatus, fsmErr := tasks.Apply(t.Status, tasks.EventVerifyStuck)
	if fsmErr != nil {
		return
	}
	t.Status = newStatus
	if err := s.PutTask(t); err != nil {
		uitheme.DangerousDialogBox(stderr, "J: tasks put: %v", err)
	}
}
