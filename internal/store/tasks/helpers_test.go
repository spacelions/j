package tasks

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/spacelions/j/internal/store")

// crockfordBase32 is the Crockford base32 alphabet used by ULID
// (uppercase, with I/L/O/U excluded). It is duplicated here on
// purpose: the test asserts the observable contract of NewTaskID
// without importing the ULID package, so a regression that swaps in
// a different alphabet still fails.
const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// openTaskStore chdirs to a fresh temp dir, runs store.EnsureProject, and
// returns a tasks-mode *Store rooted there. Cleanup is registered via
// t.Cleanup so callers do not need to close the store themselves.
func openTaskStore(t *testing.T) *Store {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	s := Open(dir)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// idsOf extracts task IDs preserving slice order. Used by sort tests.
func idsOf(tasks []Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID
	}
	return out
}

// equal reports whether two string slices are pairwise identical.
func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listAllTasks lists every task at the per-cwd tasks dir. Used by
// lifecycle tests to assert what the PersistWarn-driven helpers
// wrote. Returns nil for "no tasks dir yet" so the negative-path
// tests can distinguish "missing" from a real read error.
func listAllTasks(t *testing.T) []Task {
	t.Helper()
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	s := Open(dir)
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
	id := NewTaskID()
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	s := Open(dir)
	defer func() { _ = s.Close() }()
	begin := time.Now().UTC().Add(-time.Hour)
	end := begin.Add(time.Minute)
	task := Task{
		ID:               id,
		Status:           StatusPlanDone,
		PlanTool:         "cursor",
		PlanModel:        "sonnet-4",
		PlanResumeSession: "seed-plan-cursor",
		Summary:          summary,
		PlanBeginAt:      begin,
		PlanEndAt:        end,
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
	id := NewTaskID()
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	s := Open(dir)
	defer func() { _ = s.Close() }()
	planBegin := time.Now().UTC().Add(-2 * time.Hour)
	planEnd := planBegin.Add(time.Minute)
	workBegin := planEnd.Add(time.Minute)
	workEnd := workBegin.Add(time.Minute)
	task := Task{
		ID:               id,
		Status:           StatusWorkDone,
		PlanTool:         "cursor",
		PlanModel:        "sonnet-4",
		WorkTool:         "cursor",
		WorkModel:        "sonnet-4",
		PlanResumeSession: "seed-plan-cursor",
		WorkResumeSession: "seed-work-cursor",
		Summary:          summary,
		PlanBeginAt:      planBegin,
		PlanEndAt:        planEnd,
		WorkBeginAt:      workBegin,
		WorkEndAt:        workEnd,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}
