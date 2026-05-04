package store

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPersistWarn_OpenFailure forces bolt.Open to fail
// by parking a regular file at .j/tasks; a single warning lands on
// stderr and the helper returns the underlying error.
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
	err := PersistWarn(&stderr, Task{ID: "x", Status: StatusPlanDone})
	if err == nil {
		t.Fatal("PersistWarn should propagate the underlying open error")
	}
	if errors.Is(err, ErrOpenTimeout) {
		t.Fatalf("non-timeout open failure mis-classified as ErrOpenTimeout: %v", err)
	}
	if !strings.Contains(stderr.String(), "warning: tasks") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestPersistWarn_PutError opens the layout but feeds PersistWarn a
// task with an empty ID so PutTask errors. The "tasks put" warning
// must surface on stderr and the helper must return the put error.
func TestPersistWarn_PutError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	err := PersistWarn(&stderr, Task{Status: StatusPlanDone})
	if err == nil {
		t.Fatal("PersistWarn should propagate the put error")
	}
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestPersistWarn_RoundTrip pins the happy path: a well-formed task
// is written and a subsequent ListTasks round-trips the row. The
// helper returns nil on success.
func TestPersistWarn_RoundTrip(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	id := NewTaskID()
	if err := PersistWarn(io.Discard, Task{ID: id, Status: StatusPlanning, Summary: "hello"}); err != nil {
		t.Fatalf("PersistWarn: %v", err)
	}
	got := listAllTasks(t)
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("tasks = %+v, want one row with id %q", got, id)
	}
	if got[0].Summary != "hello" {
		t.Fatalf("Summary = %q, want hello", got[0].Summary)
	}
}

// TestPersistWarn_LockedFileEmitsRefinedBanner pins the new timeout
// branch: when the bbolt file is already locked by another open
// handle, PersistWarn renders the refined `■ J: cannot write to
// database` line on stderr (no leaky path / errno) and returns
// ErrOpenTimeout so the caller can suppress the follow-up
// `RunningInBackground` banner.
func TestPersistWarn_LockedFileEmitsRefinedBanner(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := EnsureProject(); err != nil {
		t.Fatalf("EnsureProject: %v", err)
	}
	path, err := DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	holder, err := Open(path)
	if err != nil {
		t.Fatalf("Open(holder): %v", err)
	}
	t.Cleanup(func() { _ = holder.Close() })

	var stderr bytes.Buffer
	err = PersistWarn(&stderr, Task{ID: NewTaskID(), Status: StatusPlanning})
	if !errors.Is(err, ErrOpenTimeout) {
		t.Fatalf("err = %v, want ErrOpenTimeout", err)
	}
	if !strings.Contains(stderr.String(), "■ J: cannot write to database") {
		t.Fatalf("stderr = %q, want refined banner glyph + message", stderr.String())
	}
	if strings.Contains(stderr.String(), "warning: tasks db") {
		t.Fatalf("stderr should not contain the legacy `warning: tasks db` line: %q", stderr.String())
	}
}

// TestPersistWarn_PathFailurePropagates exercises the
// DefaultTasksDBPath error branch by removing cwd out from under the
// helper. The wrapped error surfaces both on stderr (legacy wording)
// and via the return value.
func TestPersistWarn_PathFailurePropagates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipped in -short mode")
	}
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	t.Setenv("PWD", "")
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd unexpectedly succeeded; cannot drive the path-resolve failure on this OS")
	}
	var stderr bytes.Buffer
	if err := PersistWarn(&stderr, Task{ID: "x", Status: StatusPlanDone}); err == nil {
		t.Fatal("PersistWarn should propagate the path-resolve error")
	}
	if !strings.Contains(stderr.String(), "warning: tasks path") {
		t.Fatalf("stderr = %q, want tasks-path warning", stderr.String())
	}
}
