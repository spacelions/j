package tasklog

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// mustInit lays down the per-project `.j/` layout so OpenTaskLog and
// PersistWarn can open `<cwd>/.j/tasks/list.db` without relying on
// pre-flight being triggered by the cobra wiring.
func mustInit(t *testing.T) {
	t.Helper()
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
}

// readTasks opens the per-cwd tasks DB, lists every task, and closes
// the store. Used by PersistWarn tests to assert the row landed.
func readTasks(t *testing.T) []store.Task {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	s, err := store.Open(path)
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

// TestOpenTaskLog_PathFailureWarns forces DefaultTasksDBPath to fail
// (via HOME being unreadable) by parking a regular file at .j/tasks
// so the bolt.Open call errors. The helper must surface a single
// warning line on stderr and return ok=false.
func TestOpenTaskLog_OpenFailureWarns(t *testing.T) {
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
	s, ok := OpenTaskLog(&stderr)
	if ok {
		_ = s.Close()
		t.Fatalf("expected ok=false, got store=%v", s)
	}
	if !strings.Contains(stderr.String(), "warning: tasks") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestOpenTaskLog_Success returns a usable store handle the caller
// owns. The store must be closable without error.
func TestOpenTaskLog_Success(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	s, ok := OpenTaskLog(io.Discard)
	if !ok {
		t.Fatal("expected ok=true on initialised layout")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestPersistWarn_OpenFailure forces OpenTaskLog to fail by parking
// a regular file at .j/tasks; PersistWarn must be a silent no-op
// (the warning is emitted by OpenTaskLog itself).
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
	PersistWarn(&stderr, store.Task{ID: "x", Status: store.StatusPlanDone})
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want tasks warning from OpenTaskLog", stderr.String())
	}
}

// TestPersistWarn_PutError opens then closes the store so the
// subsequent PutTask inside PersistWarn fails; the warning must
// surface on stderr. A task with an empty ID reliably fails the
// PutTask validation without needing a corrupted bucket.
func TestPersistWarn_PutError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	var stderr bytes.Buffer
	PersistWarn(&stderr, store.Task{Status: store.StatusPlanDone})
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestPersistWarn_RoundTrip pins the happy path: a well-formed task
// is written, and a subsequent ListTasks round-trips the row.
func TestPersistWarn_RoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := store.NewTaskID()
	PersistWarn(io.Discard, store.Task{ID: id, Status: store.StatusPlanning, Summary: "hello"})
	got := readTasks(t)
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("tasks = %+v, want one row with id %q", got, id)
	}
	if got[0].Summary != "hello" {
		t.Fatalf("Summary = %q, want hello", got[0].Summary)
	}
}
