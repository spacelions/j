package store

import (
	"errors"
	"os"
	"testing"
	"time"
)

// crockfordBase32 is the Crockford base32 alphabet used by ULID
// (uppercase, with I/L/O/U excluded). It is duplicated here on
// purpose: the test asserts the observable contract of NewTaskID
// without importing the ULID package, so a regression that swaps in
// a different alphabet still fails.
const crockfordBase32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ptr returns &v; used inline to assemble Task pointer-typed fields in
// fixtures so the call sites stay readable.
func ptr[T any](v T) *T { return &v }

// openTaskStore chdirs to a fresh temp dir, runs EnsureProject, and
// returns an opened *Store rooted there. Cleanup is registered via
// t.Cleanup so callers do not need to close the store themselves.
func openTaskStore(t *testing.T) *Store {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
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

// listAllTasks opens the per-cwd tasks DB, lists every task, and
// closes the store. Used by lifecycle tests to assert what the
// PersistWarn-driven helpers wrote. Returns nil for "no DB yet" so
// the negative-path tests can distinguish "file missing" from a
// real bbolt error.
func listAllTasks(t *testing.T) []Task {
	t.Helper()
	path, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	return got
}

// seedPlanDoneTask seeds a `plan-done` row for the work / verify
// lifecycle tests. The id is returned so callers can look the row
// back up. Use after t.Chdir(t.TempDir()) + EnsureProject().
func seedPlanDoneTask(t *testing.T, summary string) string {
	t.Helper()
	id := NewTaskID()
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	begin := time.Now().UTC().Add(-time.Hour)
	end := begin.Add(time.Minute)
	task := Task{
		ID:               id,
		Status:           StatusPlanDone,
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "seed-plan-cursor",
		Summary:          summary,
		PlanBeginAt:      &begin,
		PlanEndAt:        &end,
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
	dbPath, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	planBegin := time.Now().UTC().Add(-2 * time.Hour)
	planEnd := planBegin.Add(time.Minute)
	workBegin := planEnd.Add(time.Minute)
	workEnd := workBegin.Add(time.Minute)
	task := Task{
		ID:               id,
		Status:           StatusWorkDone,
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "seed-plan-cursor",
		WorkResumeCursor: "seed-work-cursor",
		Summary:          summary,
		PlanBeginAt:      &planBegin,
		PlanEndAt:        &planEnd,
		WorkBeginAt:      &workBegin,
		WorkEndAt:        &workEnd,
	}
	if err := s.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}
