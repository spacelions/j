package lifecycle

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
)

// listAllTasks lists every task at the per-cwd tasks dir. Used by
// lifecycle tests to assert what the PersistWarn-driven helpers
// wrote. Returns nil for "no tasks dir yet" so the negative-path
// tests can distinguish "missing" from a real read error.
func listAllTasks(t *testing.T) []tasks.Task {
	t.Helper()
	dir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	s := tasks.Open(dir)
	defer func() { _ = s.Close() }()
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	return got
}

// seedPlanDoneTask seeds a `plan-done` row for the work / verify
// lifecycle tests. The id is returned so callers can look the row
// back up. Use after t.Chdir(t.TempDir()) + store.EnsureProject().
func seedPlanDoneTask(t *testing.T, summary string) string {
	t.Helper()
	id := tasks.NewTaskID()
	dir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	s := tasks.Open(dir)
	defer func() { _ = s.Close() }()
	begin := time.Now().UTC().Add(-time.Hour)
	end := begin.Add(time.Minute)
	task := tasks.Task{
		ID:                id,
		Status:            tasks.StatusPlanDone,
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		PlanResumeSession: "seed-plan-cursor",
		Summary:           summary,
		PlanBeginAt:       begin,
		PlanEndAt:         end,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}

// seedWorkDoneTask seeds a `work-done` row for the verify lifecycle
// tests. Mirrors seedPlanDoneTask's shape but with the work-phase
// timestamps and resume cursor populated.
func seedWorkDoneTask(t *testing.T, summary string) string {
	t.Helper()
	id := tasks.NewTaskID()
	dir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	s := tasks.Open(dir)
	defer func() { _ = s.Close() }()
	planBegin := time.Now().UTC().Add(-2 * time.Hour)
	planEnd := planBegin.Add(time.Minute)
	workBegin := planEnd.Add(time.Minute)
	workEnd := workBegin.Add(time.Minute)
	task := tasks.Task{
		ID:                id,
		Status:            tasks.StatusWorkDone,
		PlanTool:          "cursor",
		PlanModel:         "sonnet-4",
		WorkTool:          "cursor",
		WorkModel:         "sonnet-4",
		PlanResumeSession: "seed-plan-cursor",
		WorkResumeSession: "seed-work-cursor",
		Summary:           summary,
		PlanBeginAt:       planBegin,
		PlanEndAt:         planEnd,
		WorkBeginAt:       workBegin,
		WorkEndAt:         workEnd,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}

// seedPlanApprovalDisabled writes plan_requires_approval=false to the
// project settings store so PlanLifecycle.Finish(nil) uses EventPlanDone
// instead of the default EventPlanAwaitApproval. Call after EnsureProject.
func seedPlanApprovalDisabled(t *testing.T) {
	t.Helper()
	seedPlanApproval(t, "false")
}

// seedPlanApprovalEnabled writes plan_requires_approval=true so
// PlanLifecycle.Finish(nil) routes to EventPlanAwaitApproval.
func seedPlanApprovalEnabled(t *testing.T) {
	t.Helper()
	seedPlanApproval(t, "true")
}

func seedPlanApproval(t *testing.T, value string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open settings: %v", err)
	}
	defer s.Close()
	if err := s.Put(store.BucketProject,
		store.KeyPlanRequiresApproval, value); err != nil {
		t.Fatalf("Put plan_requires_approval: %v", err)
	}
}
