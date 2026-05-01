package work

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
)

// readTasks lists every task in the per-cwd tasks DB. Tests call this
// after Run to assert the lifecycle wrote what we expect.
func readTasks(t *testing.T) []store.Task {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatalf("DefaultTasksDBPath: %v", err)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
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

// TestBeginWorkTaskNew_RecordsRow pins the legacy import bbolt write:
// a fresh task row is created with status=working, the requested
// work-phase fields populated, and no plan-phase fields populated
// (since the legacy importer never had a plan phase).
func TestBeginWorkTaskNew_RecordsRow(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	taskID := store.NewTaskID()
	lc := beginWorkTaskNew(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", taskID, "/tmp/spec.plan.md", "# req", "plan body", "work-cursor")
	lc.finishWork(nil)
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].ID != taskID {
		t.Fatalf("tasks = %+v", tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if got.WorkResumeCursor != "work-cursor" {
		t.Fatalf("WorkResumeCursor = %q", got.WorkResumeCursor)
	}
	if got.PlanResumeCursor != "" {
		t.Fatalf("PlanResumeCursor should stay empty for legacy import: %q", got.PlanResumeCursor)
	}
	if got.WorkBeginAt == nil || got.WorkEndAt == nil {
		t.Fatalf("work timestamps missing: %+v", got)
	}
	if got.DoneAt != nil {
		t.Fatalf("DoneAt should not be set for work-done: %v", got.DoneAt)
	}
}

// TestBeginWorkTaskReuse_PreservesPlanPhase pins the bbolt-sourced
// reuse path: the existing plan-phase fields stay intact, only the
// work-phase fields and tool/model/resume-cursor are overwritten.
func TestBeginWorkTaskReuse_PreservesPlanPhase(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "seeded", "plan body", "req body")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	prePlanBegin := existing.PlanBeginAt
	prePlanEnd := existing.PlanEndAt
	preCursor := existing.PlanResumeCursor

	lc := beginWorkTaskReuse(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "gpt-5", existing, "fresh-work-cursor")
	lc.finishWork(nil)

	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("expected one row: %+v", tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusWorkDone {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.PlanResumeCursor != preCursor {
		t.Fatalf("PlanResumeCursor changed: got %q, want %q", got.PlanResumeCursor, preCursor)
	}
	if got.WorkResumeCursor != "fresh-work-cursor" {
		t.Fatalf("WorkResumeCursor = %q", got.WorkResumeCursor)
	}
	if got.InvokedModel != "gpt-5" {
		t.Fatalf("InvokedModel = %q, want gpt-5", got.InvokedModel)
	}
	if got.PlanBeginAt == nil || !got.PlanBeginAt.Equal(*prePlanBegin) {
		t.Fatalf("PlanBeginAt = %v, want %v", got.PlanBeginAt, prePlanBegin)
	}
	if got.PlanEndAt == nil || !got.PlanEndAt.Equal(*prePlanEnd) {
		t.Fatalf("PlanEndAt = %v, want %v", got.PlanEndAt, prePlanEnd)
	}
	if got.WorkBeginAt == nil || got.WorkEndAt == nil {
		t.Fatalf("work timestamps missing: %+v", got)
	}
}

// TestFinishWork_ErrorPath drives the StatusHelp branch when the
// underlying agent.Work errored.
func TestFinishWork_ErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	lc := beginWorkTaskNew(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", store.NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.finishWork(errors.New("boom"))
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusHelp {
		t.Fatalf("tasks = %+v, want one help task", tasks)
	}
	if tasks[0].DoneAt != nil {
		t.Fatalf("DoneAt should remain nil on failure: %v", tasks[0].DoneAt)
	}
}

// TestRecordBackground_StampsPIDAndPath drives the happy path of
// recordBackground for the work flow: the in-memory task row carries
// the PID and log path, status stays at working, and a stray
// finishWork is a silent no-op thanks to the closed flag.
func TestRecordBackground_StampsPIDAndPath(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	taskID := store.NewTaskID()
	lc := beginWorkTaskNew(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", taskID, "/tmp/x.plan.md", "", "body", "")
	lc.recordBackground(54321, "/tmp/agent.log")
	lc.finishWork(nil)
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d", len(tasks))
	}
	got := tasks[0]
	if got.Status != store.StatusWorking {
		t.Fatalf("Status = %q, want working", got.Status)
	}
	if got.BackgroundPID != 54321 {
		t.Fatalf("BackgroundPID = %d", got.BackgroundPID)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
}

// TestRecordBackground_ClosedShortCircuit pins the second-call
// no-op for `j work`: once finishWork has stamped the row, a
// subsequent recordBackground does nothing.
func TestRecordBackground_ClosedShortCircuit(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	taskID := store.NewTaskID()
	lc := beginWorkTaskNew(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", taskID, "/tmp/x.plan.md", "", "body", "")
	lc.finishWork(nil)
	lc.recordBackground(99999, "/tmp/should-not-stick.log")
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d", len(tasks))
	}
	got := tasks[0]
	if got.Status != store.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (closed branch)", got.BackgroundPID)
	}
}

func TestFinishWork_Idempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	lc := beginWorkTaskNew(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", store.NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	lc.finishWork(nil)
	lc.finishWork(errors.New("ignored"))
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusWorkDone {
		t.Fatalf("second finish should be a no-op: %+v", tasks)
	}
}

// TestOpenLifecycle_OpenTaskLogFails forces openTaskLog to return
// ok=false by replacing the post-init list.db file with a directory.
// Both beginWorkTaskNew and finishWork emit a warning and execution
// continues without panicking.
func TestOpenLifecycle_OpenTaskLogFails(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	lc := beginWorkTaskNew(Options{Stderr: &stderr}, &scriptedAgent{name: "cursor"}, "m", store.NewTaskID(), "/tmp/x.plan.md", "", "body", "")
	if lc == nil {
		t.Fatal("beginWorkTaskNew returned nil lifecycle")
	}
	lc.finishWork(nil)
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want tasks warning", stderr.String())
	}
}

// TestFinishWork_PutErrorWarns drives the finalize-time put warning
// by handing finishWork a task with no ID. tasklog.OpenTaskLog
// succeeds but store.PutTask rejects the empty ID, so
// tasklog.PersistWarn emits the expected warning.
func TestFinishWork_PutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	var stderr bytes.Buffer
	lc := &workLifecycle{stderr: &stderr, task: store.Task{
		Status: store.StatusWorking,
	}}
	lc.finishWork(nil)
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// TestBeginWorkTaskNew_MintsWorktreeName pins the R2 contract on the
// legacy-import path: an empty Worktree is populated via
// store.WorktreeNameFor using the cwd basename and the task summary.
func TestBeginWorkTaskNew_MintsWorktreeName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	mustInit(t)
	taskID := store.NewTaskID()
	lc := beginWorkTaskNew(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", taskID, "/tmp/x.plan.md", "# do the thing", "body", "")
	lc.finishWork(nil)
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %+v", tasks)
	}
	if tasks[0].Worktree != "myproj-do-the-thing" {
		t.Fatalf("Worktree = %q, want %q", tasks[0].Worktree, "myproj-do-the-thing")
	}
}

// TestBeginWorkTaskReuse_MintsWorktreeWhenEmpty pins the R2 contract
// on the bbolt-sourced reuse path: an empty Worktree on the existing
// row is populated during the reuse transition.
func TestBeginWorkTaskReuse_MintsWorktreeWhenEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	mustInit(t)
	id := seedPlanDoneTask(t, "hello world", "plan", "")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if existing.Worktree != "" {
		t.Fatalf("seed task already has worktree %q; test setup bug", existing.Worktree)
	}
	lc := beginWorkTaskReuse(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", existing, "cursor")
	lc.finishWork(nil)
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %+v", tasks)
	}
	// seedPlanDoneTask stores the first argument verbatim as Summary;
	// "hello world" slugifies to "hello-world".
	if tasks[0].Worktree != "myproj-hello-world" {
		t.Fatalf("Worktree = %q, want %q", tasks[0].Worktree, "myproj-hello-world")
	}
}

// TestBeginWorkTaskReuse_PreservesPreExistingWorktree pins the
// preserve-existing-value branch of fillWorktree: a pre-populated
// Worktree on the bbolt row survives the reuse transition untouched.
func TestBeginWorkTaskReuse_PreservesPreExistingWorktree(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "hello", "plan", "")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	existing.Worktree = "manual-override"
	lc := beginWorkTaskReuse(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "m", existing, "cursor")
	lc.finishWork(nil)
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %+v", tasks)
	}
	if tasks[0].Worktree != "manual-override" {
		t.Fatalf("Worktree = %q, want %q", tasks[0].Worktree, "manual-override")
	}
}

// TestBeginWorkTaskResume_LeavesWorktreeAlone pins the resume path:
// even an empty Worktree stays empty (resume never re-mints) so a
// pre-R2 task falls through to the verifier's main-checkout
// fallback, avoiding a forced migration.
func TestBeginWorkTaskResume_LeavesWorktreeAlone(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedPlanDoneTask(t, "hello", "plan", "")
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if existing.Worktree != "" {
		t.Fatalf("seed task already has worktree %q; test setup bug", existing.Worktree)
	}
	lc := beginWorkTaskResume(Options{Stderr: io.Discard}, existing)
	lc.finishWork(nil)
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %+v", tasks)
	}
	if tasks[0].Worktree != "" {
		t.Fatalf("Worktree = %q, want empty (resume leaves it alone)", tasks[0].Worktree)
	}
}
