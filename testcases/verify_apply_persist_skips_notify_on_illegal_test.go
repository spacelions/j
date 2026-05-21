package testcases_test

import (
	"errors"
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestVerify_ApplyAndPersist_IllegalTransitionLeavesUntouched pins the
// FSM-error contract of the new helper: an illegal source/event pair
// must NOT mutate t.Status, must NOT persist anything, and must NOT
// fire registered hooks. Without this guarantee the marker hook would
// log spurious "ghost" lines for transitions the FSM rejected.
func TestVerify_ApplyAndPersist_IllegalTransitionLeavesUntouched(
	t *testing.T,
) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	t.Cleanup(tasks.ResetHooksForTest)
	var fired int
	tasks.Register(func(tasks.Transition, tasks.Task) { fired++ })

	id := tasks.NewTaskID()
	if _, err := tasks.EnsureDir(id); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()

	row := tasks.Task{ID: id, Status: tasks.StatusPlanning}
	if err := s.PutTask(row); err != nil {
		t.Fatalf("seed PutTask: %v", err)
	}

	tr, err := tasks.ApplyAndPersist(s, &row, tasks.EventVerifyPass)
	if err == nil {
		t.Fatal("expected IllegalTransitionError")
	}
	var illegal tasks.IllegalTransitionError
	if !errors.As(err, &illegal) {
		t.Fatalf("err = %v, want IllegalTransitionError", err)
	}
	if tr.To != "" {
		t.Fatalf("transition.To = %q, want empty on illegal", tr.To)
	}
	if row.Status != tasks.StatusPlanning {
		t.Fatalf("row mutated on illegal transition: %q", row.Status)
	}
	if !row.DoneAt.IsZero() {
		t.Fatal("DoneAt stamped on illegal transition")
	}
	persisted, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if persisted.Status != tasks.StatusPlanning {
		t.Fatalf("persisted Status = %q, want untouched planning",
			persisted.Status)
	}
	if fired != 0 {
		t.Fatalf("hooks fired on illegal transition: %d", fired)
	}
}
