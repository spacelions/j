package tasks

import (
	"strings"
	"testing"
)

func TestApply_AllTransitionsRoundTrip(t *testing.T) {
	for _, tr := range transitions {
		got, err := Apply(tr.From, tr.Event)
		if err != nil {
			t.Errorf("Apply(%q, %q): %v", tr.From, tr.Event, err)
			continue
		}
		if got != tr.To {
			t.Errorf("Apply(%q, %q) = %q, want %q",
				tr.From, tr.Event, got, tr.To)
		}
	}
}

// TestApply_FailedAndCompletedRecoveryEdges pins the nine new edges
// that let `re-*` and `resume-*` rerun a finished task. Each entry
// here must remain in the transitions table for the corresponding
// CLI command to leave its IsLegal guard.
func TestApply_FailedAndCompletedRecoveryEdges(t *testing.T) {
	cases := []struct {
		from TaskStatus
		ev   Event
		to   TaskStatus
	}{
		{StatusPlanning, EventPlanResume, StatusPlanning},
		{StatusFailed, EventPlanResume, StatusPlanning},
		{StatusFailed, EventWorkResume, StatusWorking},
		{StatusFailed, EventVerifyResume, StatusVerifying},
		{StatusCompleted, EventPlanRestart, StatusPlanning},
		{StatusCompleted, EventPlanResume, StatusPlanning},
		{StatusCompleted, EventWorkRestart, StatusWorking},
		{StatusCompleted, EventWorkResume, StatusWorking},
		{StatusCompleted, EventVerifyRestart, StatusVerifying},
		{StatusCompleted, EventVerifyResume, StatusVerifying},
	}
	for _, c := range cases {
		got, err := Apply(c.from, c.ev)
		if err != nil {
			t.Errorf("Apply(%q, %q): %v", c.from, c.ev, err)
			continue
		}
		if got != c.to {
			t.Errorf("Apply(%q, %q) = %q, want %q",
				c.from, c.ev, got, c.to)
		}
	}
}

func TestApply_IllegalTransition(t *testing.T) {
	_, err := Apply(StatusPlanning, EventWorkBegin)
	if err == nil {
		t.Fatal("expected error for illegal transition")
	}
	var ite IllegalTransitionError
	if !strings.Contains(err.Error(), "illegal transition") {
		t.Errorf("error %q does not look like IllegalTransitionError", err.Error())
	}
	_ = ite
}

func TestIsLegal(t *testing.T) {
	if !IsLegal(StatusPlanning, EventPlanDone) {
		t.Error("IsLegal(planning, plan_done) = false, want true")
	}
	if IsLegal(StatusPlanning, EventWorkBegin) {
		t.Error("IsLegal(planning, work_begin) = true, want false")
	}
	if !IsLegal("", EventPlanBegin) {
		t.Error(`IsLegal("", plan_begin) = false, want true`)
	}
}

// TestApply_ForegroundPlanNeedsClarification pins the foreground
// planner-exit edge: from `planning` the new event lands the row in
// `needs-clarification`, mirroring the existing reaper-event entry.
func TestApply_ForegroundPlanNeedsClarification(t *testing.T) {
	got, err := Apply(StatusPlanning, EventPlanNeedsClarification)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != StatusNeedsClarification {
		t.Fatalf("Apply = %q, want needs-clarification", got)
	}
	if !IsLegal(StatusPlanning, EventPlanNeedsClarification) {
		t.Fatal("IsLegal returned false for foreground edge")
	}
}

func TestIsLegal_PlanResumeFromPendingApproval(t *testing.T) {
	if !IsLegal(StatusPlanPendingApproval, EventPlanResume) {
		t.Error(
			"IsLegal(plan-pending-approval, plan_resume) = false, want true")
	}
	got, err := Apply(StatusPlanPendingApproval, EventPlanResume)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got != StatusPlanning {
		t.Errorf("Apply(...) = %q, want %q", got, StatusPlanning)
	}
}

func TestIsLegal_DoesNotFireHooks(t *testing.T) {
	ResetHooksForTest()
	fired := false
	Register(func(tr Transition, task Task) { fired = true })

	if !IsLegal(StatusPlanning, EventPlanDone) {
		t.Fatal("IsLegal returned false")
	}
	if fired {
		t.Error("IsLegal fired hooks — Apply should not fire hooks either")
	}
	ResetHooksForTest()
}

func TestApply_DoesNotFireHooks(t *testing.T) {
	ResetHooksForTest()
	fired := false
	Register(func(tr Transition, task Task) { fired = true })

	_, err := Apply(StatusPlanning, EventPlanDone)
	if err != nil {
		t.Fatal(err)
	}
	if fired {
		t.Error("Apply fired hooks — only Notify should fire hooks")
	}
	ResetHooksForTest()
}

func TestLegalEvents(t *testing.T) {
	events := LegalEvents(StatusPlanning)
	if len(events) == 0 {
		t.Fatal("LegalEvents(planning) returned empty")
	}
	seen := map[Event]bool{}
	for _, e := range events {
		seen[e] = true
	}
	want := []Event{
		EventPlanDone, EventPlanAwaitApproval,
		EventPlanError, EventPlanQuit,
		EventPlanNeedsClarification,
		EventReaperPlanDone, EventReaperPlanAwaitApproval,
		EventReaperPlanFail, EventReaperPlanNeedsClarification,
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("LegalEvents(planning) missing %q", w)
		}
	}
}

func TestLegalEvents_NewTask(t *testing.T) {
	events := LegalEvents("")
	if len(events) != 1 || events[0] != EventPlanBegin {
		t.Fatalf("LegalEvents(\"\") = %v, want [plan_begin]", events)
	}
}

func TestIllegalTransitionError_Error(t *testing.T) {
	err := IllegalTransitionError{From: StatusPlanning, Event: EventWorkBegin}
	msg := err.Error()
	if !strings.Contains(msg, string(StatusPlanning)) {
		t.Errorf("error %q missing from-status", msg)
	}
	if !strings.Contains(msg, string(EventWorkBegin)) {
		t.Errorf("error %q missing event", msg)
	}
}

func TestEveryValidStatusInTransitions(t *testing.T) {
	inTable := map[TaskStatus]bool{}
	for _, tr := range transitions {
		inTable[tr.From] = true
		inTable[tr.To] = true
	}
	inTable[""] = true

	for _, s := range allStatuses() {
		if !inTable[s] {
			t.Errorf("status %q not found as source or dest in transitions", s)
		}
	}
}

func allStatuses() []TaskStatus {
	return []TaskStatus{
		StatusPlanning, StatusPlanPendingApproval, StatusPlanDone,
		StatusWorking, StatusWorkDone, StatusVerifying,
		StatusNeedsClarification, StatusCompleted, StatusFailed,
		StatusHelp,
	}
}

func TestNotify_RegistrationOrder(t *testing.T) {
	ResetHooksForTest()
	var order []int
	Register(func(tr Transition, task Task) { order = append(order, 1) })
	Register(func(tr Transition, task Task) { order = append(order, 2) })
	Register(func(tr Transition, task Task) { order = append(order, 3) })

	tr := Transition{StatusPlanning, EventPlanDone, StatusPlanDone}
	Notify(tr, Task{})
	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Errorf("hook order = %v, want [1 2 3]", order)
	}

	ResetHooksForTest()
}

func TestNotify_FiresWithTask(t *testing.T) {
	ResetHooksForTest()
	var received Task
	Register(func(tr Transition, task Task) { received = task })

	tr := Transition{StatusPlanning, EventPlanDone, StatusPlanDone}
	Notify(tr, Task{ID: "test-id", Status: StatusPlanDone})
	if received.ID != "test-id" {
		t.Errorf("hook received task ID %q, want test-id", received.ID)
	}
	if received.Status != StatusPlanDone {
		t.Errorf("hook received status %q, want plan-done", received.Status)
	}
	ResetHooksForTest()
}

func TestNotify_PanicIsolation(t *testing.T) {
	ResetHooksForTest()
	var secondFired bool
	Register(func(tr Transition, task Task) { panic("boom") })
	Register(func(tr Transition, task Task) { secondFired = true })

	tr := Transition{StatusPlanning, EventPlanDone, StatusPlanDone}
	Notify(tr, Task{})
	if !secondFired {
		t.Error("second hook did not fire after first panicked")
	}

	ResetHooksForTest()
}

func TestResetHooksForTest(t *testing.T) {
	ResetHooksForTest()
	var fired bool
	Register(func(tr Transition, task Task) { fired = true })

	ResetHooksForTest()

	tr := Transition{StatusPlanning, EventPlanDone, StatusPlanDone}
	Notify(tr, Task{})
	if fired {
		t.Error("hook fired after ResetHooksForTest")
	}
}

func TestTransitionTable_NoDuplicateEdges(t *testing.T) {
	seen := map[string]bool{}
	for _, tr := range transitions {
		key := string(tr.From) + "|" + string(tr.Event)
		if seen[key] {
			t.Errorf("duplicate transition: from=%q event=%q", tr.From, tr.Event)
		}
		seen[key] = true
	}
}

func TestTransitionTable_TerminalStates(t *testing.T) {
	for _, tr := range transitions {
		switch tr.From {
		case StatusFailed, StatusCompleted, StatusHelp:
			if !strings.Contains(string(tr.Event), "restart") &&
				!strings.Contains(string(tr.Event), "resume") {
				t.Errorf("%s state has non-restart/resume edge: %q",
					tr.From, tr.Event)
			}
		}
	}
}

func TestTransitionTable_BeginEdges(t *testing.T) {
	for _, tr := range transitions {
		if !strings.Contains(string(tr.Event), "begin") {
			continue
		}
		switch tr.To {
		case StatusPlanning, StatusWorking, StatusVerifying:
		default:
			t.Errorf("begin event %q goes to non-active %q", tr.Event, tr.To)
		}
	}
}

func TestTransitionTable_DoneEdges(t *testing.T) {
	for _, tr := range transitions {
		e := string(tr.Event)
		if !strings.Contains(e, "done") && !strings.Contains(e, "pass") {
			continue
		}
		switch tr.To {
		case StatusPlanDone, StatusWorkDone, StatusCompleted, StatusFailed:
		case StatusPlanPendingApproval:
		default:
			t.Errorf("done/pass event %q goes to unexpected %q", tr.Event, tr.To)
		}
	}
}

func TestExistingTest_StatusBackwardCompat(t *testing.T) {
	// task.toml files written before this refactor must load cleanly.
	// The old status constants must still be valid.
	for _, s := range []TaskStatus{
		"planning", "plan-done", "working", "work-done",
		"verifying", "failed", "completed", "help",
	} {
		if !s.Valid() {
			t.Errorf("old status %q is no longer valid", s)
		}
	}
}

func TestTask_PullRequestURLRoundTrip(t *testing.T) {
	// TOML omitempty: empty PR URL must not appear in output.
	s, err := OpenDefault()
	if err != nil {
		t.Skipf("open: %v", err)
	}
	defer s.Close()
	task := Task{
		ID:             "01KRTEST",
		Status:         StatusWorkDone,
		PullRequestURL: "https://github.com/owner/repo/pull/42",
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.GetTask("01KRTEST")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PullRequestURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("PullRequestURL = %q, want github PR URL", got.PullRequestURL)
	}
	// Empty PR URL must also round-trip.
	task2 := Task{ID: "01KRTEST2", Status: StatusWorkDone}
	if err := s.PutTask(task2); err != nil {
		t.Fatalf("put: %v", err)
	}
	got2, err := s.GetTask("01KRTEST2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got2.PullRequestURL != "" {
		t.Errorf("empty PullRequestURL = %q, want empty", got2.PullRequestURL)
	}
}
