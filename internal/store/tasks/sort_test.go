package tasks

import (
	"testing"
	"time"
)

func TestSortTasks_ActiveFirstThenByDoneAtDesc(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)

	tasks := []Task{
		{ID: "z-done-old", Status: StatusCompleted, DoneAt: ptr(t1)},
		{ID: "a-done-new", Status: StatusCompleted, DoneAt: ptr(t3)},
		{ID: "m-plandone", Status: StatusPlanDone, DoneAt: ptr(t2)},
		{ID: "active-2", Status: StatusWorking},
		{ID: "active-1", Status: StatusPlanning},
		{ID: "active-3", Status: StatusVerifying},
		{ID: "active-4", Status: StatusHelp},
	}
	SortTasks(tasks)

	wantIDs := []string{
		"active-1", "active-2", "active-3", "active-4",
		"a-done-new", "m-plandone", "z-done-old",
	}
	for i, id := range wantIDs {
		if tasks[i].ID != id {
			t.Fatalf("tasks[%d].ID = %q, want %q (got order: %v)", i, tasks[i].ID, id, idsOf(tasks))
		}
	}
}

// TestSortTasks_FallbackTimes drives every branch in taskFallbackTime
// (DoneAt, VerifyEndAt, WorkEndAt, PlanEndAt, zero).
func TestSortTasks_FallbackTimes(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	t4 := t3.Add(time.Hour)
	tasks := []Task{
		{ID: "plan-only", Status: StatusPlanDone, PlanEndAt: ptr(t1)},
		{ID: "work-end", Status: StatusWorkDone, WorkEndAt: ptr(t3)},
		{ID: "verify-end", Status: StatusVerifyDone, VerifyEndAt: ptr(t4)},
		{ID: "no-time", Status: StatusPlanDone},
		{ID: "done", Status: StatusCompleted, DoneAt: ptr(t2)},
	}
	SortTasks(tasks)
	want := []string{"verify-end", "work-end", "done", "plan-only", "no-time"}
	if got := idsOf(tasks); !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestSortTasks_TieBreakers drives the equal-time path (ID descending
// for inactive) and the equal-active path (ID ascending).
func TestSortTasks_TieBreakers(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tasks := []Task{
		{ID: "inactive-a", Status: StatusCompleted, DoneAt: &at},
		{ID: "inactive-b", Status: StatusCompleted, DoneAt: &at},
		{ID: "active-b", Status: StatusWorking},
		{ID: "active-a", Status: StatusPlanning},
	}
	SortTasks(tasks)
	want := []string{"active-a", "active-b", "inactive-b", "inactive-a"}
	if got := idsOf(tasks); !equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
