package orchestrator

import (
	"errors"
	"io"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store/tasks"
)

// finaliseVerifyFailIfStuck flips a row stuck at `verifying` to
// `failed` after the SequentialAgent iterator drains. Routing through
// ApplyAndPersist guarantees the marker hook sees the transition and
// keeps DoneAt-stamping centralised.
//
// IllegalTransitionError on already-terminal rows (completed / failed
// / help / etc.) is the silent no-op path; PutTask errors surface as a
// warning so a stuck row never fails silently.
func finaliseVerifyFailIfStuck(stderr io.Writer, taskID string) {
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()
	t, err := s.GetTask(taskID)
	if err != nil {
		return
	}
	_, err = tasks.ApplyAndPersist(s, &t, tasks.EventVerifyStuck)
	if err == nil {
		return
	}
	var illegal tasks.IllegalTransitionError
	if errors.As(err, &illegal) {
		return
	}
	uitheme.DangerousDialogBox(stderr, "J: tasks put: %v", err)
}
