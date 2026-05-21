package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestVerify_ApplyAndPersist_NotifiesAndStampsDoneAt pins the
// canonical "Apply -> mutate -> PutTask -> Notify" path that every
// migrated reaper / finaliser call site is expected to use:
//   - the task's Status flips to the FSM destination,
//   - the row is durably persisted (re-readable via GetTask),
//   - registered hooks observe the transition exactly once with the
//     post-mutation snapshot,
//   - terminal `completed` status carries a non-zero DoneAt stamp so
//     `j tasks` can distinguish completions from failures.
func TestVerify_ApplyAndPersist_NotifiesAndStampsDoneAt(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	t.Cleanup(tasks.ResetHooksForTest)
	var observed []tasks.Transition
	var observedTasks []tasks.Task
	tasks.Register(func(tr tasks.Transition, snap tasks.Task) {
		observed = append(observed, tr)
		observedTasks = append(observedTasks, snap)
	})

	id := tasks.NewTaskID()
	if _, err := tasks.EnsureDir(id); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	s := tasks.OpenDefault()
	defer func() { _ = s.Close() }()

	row := tasks.Task{ID: id, Status: tasks.StatusVerifying}
	if err := s.PutTask(row); err != nil {
		t.Fatalf("seed PutTask: %v", err)
	}

	tr, err := tasks.ApplyAndPersist(s, &row, tasks.EventVerifyPass)
	if err != nil {
		t.Fatalf("ApplyAndPersist: %v", err)
	}
	if tr.From != tasks.StatusVerifying ||
		tr.To != tasks.StatusCompleted ||
		tr.Event != tasks.EventVerifyPass {
		t.Fatalf("transition = %+v", tr)
	}
	if row.Status != tasks.StatusCompleted {
		t.Fatalf("row.Status = %q, want completed", row.Status)
	}
	if row.DoneAt.IsZero() {
		t.Fatal("DoneAt must be stamped on completed")
	}

	persisted, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if persisted.Status != tasks.StatusCompleted {
		t.Fatalf("persisted Status = %q, want completed",
			persisted.Status)
	}
	if persisted.DoneAt.IsZero() {
		t.Fatal("persisted DoneAt must be stamped on completed")
	}

	if len(observed) != 1 {
		t.Fatalf("hook fires = %d, want 1", len(observed))
	}
	if observed[0] != tr {
		t.Fatalf("observed transition = %+v, want %+v",
			observed[0], tr)
	}
	if observedTasks[0].Status != tasks.StatusCompleted {
		t.Fatalf("hook saw stale Status %q, want completed",
			observedTasks[0].Status)
	}
}
