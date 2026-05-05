package tasks

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store")

// TestNewWorkTask_RecordsRow pins the fresh work-row write: a fresh
// row at status=working, work fields populated, and no plan fields.
func TestNewWorkTask_RecordsRow(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := NewTaskID()
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", id, "/tmp/spec.plan.md", "# req", "plan body", "work-cursor")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.ID != id || got.Status != StatusWorkDone {
		t.Fatalf("got = %+v", got)
	}
	if got.WorkResumeCursor != "work-cursor" {
		t.Fatalf("WorkResumeCursor = %q", got.WorkResumeCursor)
	}
	if got.PlanResumeCursor != "" {
		t.Fatalf("PlanResumeCursor should stay empty for fresh work row: %q", got.PlanResumeCursor)
	}
	if got.WorkBeginAt == nil || got.WorkEndAt == nil {
		t.Fatalf("work timestamps missing: %+v", got)
	}
	if got.DoneAt != nil {
		t.Fatalf("DoneAt should not be set for work-done: %v", got.DoneAt)
	}
}

// TestTask_BeginWorkReuse_PreservesPlanPhase pins the bbolt-sourced
// reuse path: the existing plan-phase fields stay intact.
func TestTask_BeginWorkReuse_PreservesPlanPhase(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "seeded")
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	prePlanBegin := existing.PlanBeginAt
	prePlanEnd := existing.PlanEndAt
	preCursor := existing.PlanResumeCursor

	lc := existing.BeginWorkReuse(io.Discard, "cursor", "gpt-5", "fresh-work-cursor")
	lc.Finish(nil)

	got := listAllTasks(t)[0]
	if got.Status != StatusWorkDone {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.PlanResumeCursor != preCursor {
		t.Fatalf("PlanResumeCursor changed: got %q, want %q", got.PlanResumeCursor, preCursor)
	}
	if got.WorkResumeCursor != "fresh-work-cursor" {
		t.Fatalf("WorkResumeCursor = %q", got.WorkResumeCursor)
	}
	if got.InvokedModel != "gpt-5" {
		t.Fatalf("InvokedModel = %q", got.InvokedModel)
	}
	if got.PlanBeginAt == nil || !got.PlanBeginAt.Equal(*prePlanBegin) {
		t.Fatalf("PlanBeginAt = %v", got.PlanBeginAt)
	}
	if got.PlanEndAt == nil || !got.PlanEndAt.Equal(*prePlanEnd) {
		t.Fatalf("PlanEndAt = %v", got.PlanEndAt)
	}
}

// TestWorkLifecycle_FinishErrorPath drives the StatusHelp branch.
func TestWorkLifecycle_FinishErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "m", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.Finish(errors.New("boom"))
	got := listAllTasks(t)[0]
	if got.Status != StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
	if got.DoneAt != nil {
		t.Fatalf("DoneAt should remain nil on failure: %v", got.DoneAt)
	}
}

// TestWorkLifecycle_RecordBackground_StampsPIDAndPath drives the
// happy path of RecordBackground for the work flow.
func TestWorkLifecycle_RecordBackground_StampsPIDAndPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.RecordBackground(54321, "/tmp/agent.log")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Status != StatusWorking {
		t.Fatalf("Status = %q, want working", got.Status)
	}
	if got.BackgroundPID != 54321 {
		t.Fatalf("BackgroundPID = %d", got.BackgroundPID)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
}

// TestWorkLifecycle_RecordBackground_ClosedShortCircuit pins the
// second-call no-op for the work flow.
func TestWorkLifecycle_RecordBackground_ClosedShortCircuit(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.Finish(nil)
	lc.RecordBackground(99999, "/tmp/should-not-stick.log")
	got := listAllTasks(t)[0]
	if got.Status != StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0", got.BackgroundPID)
	}
}

// TestWorkLifecycle_FinishIdempotent pins the second-Finish no-op.
func TestWorkLifecycle_FinishIdempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.Finish(nil)
	lc.Finish(errors.New("ignored"))
	tasks := listAllTasks(t)
	if len(tasks) != 1 || tasks[0].Status != StatusWorkDone {
		t.Fatalf("second finish should be a no-op: %+v", tasks)
	}
}

// TestNewWorkTask_OpenFails forces PutTask's mkdir of the per-task
// directory to fail by replacing `.j/tasks` with a regular file.
// NewWorkTask and Finish each emit a warning and continue.
func TestNewWorkTask_OpenFails(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	path, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	lc := NewWorkTask(&stderr, "cursor", "m", NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	if lc == nil {
		t.Fatal("NewWorkTask returned nil")
	}
	lc.Finish(nil)
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestWorkLifecycle_FinishPutErrorWarns drives the put warning by
// handing Finish a task with no ID.
func TestWorkLifecycle_FinishPutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := &WorkLifecycle{stderr: &stderr, task: Task{Status: StatusWorking}}
	lc.Finish(nil)
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// TestNewWorkTask_MintsWorktreeName pins the worktree slug derivation
// on the legacy-import path: the cwd basename + summary slug.
func TestNewWorkTask_MintsWorktreeName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "m", NewTaskID(), "/tmp/x.plan.md", "# do the thing", "body", "")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "myproj-do-the-thing" {
		t.Fatalf("Worktree = %q", got.Worktree)
	}
}

// TestTask_BeginWorkReuse_MintsWorktreeWhenEmpty pins the
// reuse-mint-on-empty branch.
func TestTask_BeginWorkReuse_MintsWorktreeWhenEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "hello world")
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if existing.Worktree != "" {
		t.Fatalf("seed already has worktree %q", existing.Worktree)
	}
	lc := existing.BeginWorkReuse(io.Discard, "cursor", "m", "cursor")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "myproj-hello-world" {
		t.Fatalf("Worktree = %q", got.Worktree)
	}
}

// TestTask_BeginWorkReuse_PreservesPreExistingWorktree pins the
// preserve-existing-value branch of fillWorktree.
func TestTask_BeginWorkReuse_PreservesPreExistingWorktree(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "hello")
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	existing.Worktree = "manual-override"
	lc := existing.BeginWorkReuse(io.Discard, "cursor", "m", "cursor")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "manual-override" {
		t.Fatalf("Worktree = %q", got.Worktree)
	}
}

// TestTask_BeginWorkResume_LeavesWorktreeAlone pins that resume never
// re-mints Worktree (a pre-R2 task stays empty so the verifier falls
// back to the main checkout).
func TestTask_BeginWorkResume_LeavesWorktreeAlone(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "hello")
	dbPath, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if existing.Worktree != "" {
		t.Fatalf("seed already has worktree %q", existing.Worktree)
	}
	lc := existing.BeginWorkResume(io.Discard)
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "" {
		t.Fatalf("Worktree = %q, want empty", got.Worktree)
	}
}

// TestWorkLifecycle_Task returns a value copy of the in-memory task
// row so callers can read freshly-minted Worktree without poking at
// the unexported field.
func TestWorkLifecycle_Task(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "m", NewTaskID(), "/tmp/x.plan.md", "# do the thing", "body", "")
	if got := lc.Task(); got.Worktree != "myproj-do-the-thing" {
		t.Fatalf("Task().Worktree = %q", got.Worktree)
	}
}
