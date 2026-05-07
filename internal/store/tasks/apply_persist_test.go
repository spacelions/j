package tasks

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestApplyAndPersist_HappyPath pins the canonical apply → persist →
// notify flow: t.Status flips, the row reaches disk, the registered
// hook fires once with the right transition, and StampTerminal stamps
// DoneAt for StatusCompleted.
func TestApplyAndPersist_HappyPath(t *testing.T) {
	s := openTaskStore(t)
	t.Cleanup(ResetHooksForTest)
	var got []Transition
	Register(func(tr Transition, _ Task) {
		got = append(got, tr)
	})
	row := Task{ID: NewTaskID(), Status: StatusVerifying}
	if err := s.PutTask(row); err != nil {
		t.Fatalf("seed PutTask: %v", err)
	}
	tr, err := ApplyAndPersist(s, &row, EventVerifyPass)
	if err != nil {
		t.Fatalf("ApplyAndPersist: %v", err)
	}
	if tr.From != StatusVerifying || tr.To != StatusCompleted {
		t.Fatalf("transition = %+v", tr)
	}
	if row.Status != StatusCompleted {
		t.Fatalf("row.Status = %q", row.Status)
	}
	if row.DoneAt.IsZero() {
		t.Fatal("DoneAt should be stamped on completed")
	}
	persisted, err := s.GetTask(row.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if persisted.Status != StatusCompleted {
		t.Fatalf("persisted Status = %q", persisted.Status)
	}
	if len(got) != 1 || got[0].Event != EventVerifyPass {
		t.Fatalf("hooks captured = %+v", got)
	}
}

// TestApplyAndPersist_IllegalTransition pins the FSM-error branch:
// the helper leaves *t and the store untouched and never fires hooks.
func TestApplyAndPersist_IllegalTransition(t *testing.T) {
	s := openTaskStore(t)
	t.Cleanup(ResetHooksForTest)
	var fired int
	Register(func(Transition, Task) { fired++ })
	row := Task{ID: NewTaskID(), Status: StatusPlanning}
	tr, err := ApplyAndPersist(s, &row, EventVerifyPass)
	if err == nil {
		t.Fatal("expected IllegalTransitionError")
	}
	var illegal IllegalTransitionError
	if !errors.As(err, &illegal) {
		t.Fatalf("err = %v, want IllegalTransitionError", err)
	}
	if tr.To != "" {
		t.Fatalf("transition.To = %q, want empty", tr.To)
	}
	if row.Status != StatusPlanning {
		t.Fatalf("row mutated on illegal transition: %q", row.Status)
	}
	if fired != 0 {
		t.Fatalf("hooks fired on illegal transition: %d", fired)
	}
}

// TestApplyAndPersist_PutErrorSkipsNotify pins the put-error branch:
// the in-memory mutation reflects the would-be transition (so reaper
// callers can still surface the new status to the user) but Notify
// must not fire when the row never reached disk.
func TestApplyAndPersist_PutErrorSkipsNotify(t *testing.T) {
	t.Cleanup(ResetHooksForTest)
	var fired int
	Register(func(Transition, Task) { fired++ })
	parent := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(parent, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Open(parent)
	row := Task{ID: NewTaskID(), Status: StatusVerifying}
	tr, err := ApplyAndPersist(s, &row, EventVerifyPass)
	if err == nil {
		t.Fatal("expected PutTask error")
	}
	if tr.To != StatusCompleted {
		t.Fatalf("tr.To = %q, want completed", tr.To)
	}
	if row.Status != StatusCompleted {
		t.Fatalf("row.Status = %q, want completed", row.Status)
	}
	if fired != 0 {
		t.Fatalf("hooks fired on put failure: %d", fired)
	}
}

// TestStampTerminal_OnlyCompleted pins the policy: DoneAt is stamped
// on `completed` and left alone on every other status, including
// `failed`. Centralising this rule lets verify.Finish drop its inline
// branch without behaviour change.
func TestStampTerminal_OnlyCompleted(t *testing.T) {
	tcs := []struct {
		status TaskStatus
		stamp  bool
	}{
		{StatusCompleted, true},
		{StatusFailed, false},
		{StatusHelp, false},
		{StatusVerifying, false},
		{StatusPlanning, false},
	}
	for _, c := range tcs {
		row := Task{Status: c.status}
		StampTerminal(&row)
		if c.stamp && row.DoneAt.IsZero() {
			t.Errorf("StampTerminal(%q) DoneAt zero", c.status)
		}
		if !c.stamp && !row.DoneAt.IsZero() {
			t.Errorf("StampTerminal(%q) stamped DoneAt", c.status)
		}
	}
}
