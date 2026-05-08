package tasks

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// TestPersistWarn_OpenFailure forces bolt.Open to fail
// by parking a regular file at .j/tasks; a single warning lands on
// stderr and execution returns silently.
func TestPersistWarn_OpenFailure(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	jdir := filepath.Join(dir, ".j")
	if err := os.MkdirAll(jdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jdir, "tasks"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	PersistWarn(&stderr, Task{ID: "x", Status: StatusPlanDone})
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestPersistWarn_PutError opens the layout but feeds PersistWarn a
// task with an empty ID so PutTask errors. The "tasks put" warning
// must surface on stderr.
func TestPersistWarn_PutError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	PersistWarn(&stderr, Task{Status: StatusPlanDone})
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestPersistWarn_RoundTrip pins the happy path: a well-formed task
// is written and a subsequent ListTasks round-trips the row.
func TestPersistWarn_RoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := NewTaskID()
	PersistWarn(io.Discard, Task{ID: id, Status: StatusPlanning, Summary: "hello"})
	got := listAllTasks(t)
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("tasks = %+v, want one row with id %q", got, id)
	}
	if got[0].Summary != "hello" {
		t.Fatalf("Summary = %q, want hello", got[0].Summary)
	}
}
