package lifecycle

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/util/agentlog"
)

// TestNewWorkTask_RecordsRow pins the fresh work-row write: a fresh
// row at status=working, work fields populated, and no plan fields.
func TestNewWorkTask_RecordsRow(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := tasks.NewTaskID()
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", id, "/tmp/spec.plan.md", "# req", "plan body", "work-cursor", "")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.ID != id || got.Status != tasks.StatusWorkDone {
		t.Fatalf("got = %+v", got)
	}
	if got.WorkResumeSession != "work-cursor" {
		t.Fatalf("WorkResumeSession = %q", got.WorkResumeSession)
	}
	if got.PlanResumeSession != "" {
		t.Fatalf("PlanResumeSession should stay empty for fresh work row: %q", got.PlanResumeSession)
	}
	if got.WorkBeginAt.IsZero() || got.WorkEndAt.IsZero() {
		t.Fatalf("work timestamps missing: %+v", got)
	}
	if !got.DoneAt.IsZero() {
		t.Fatalf("DoneAt should not be set for work-done: %v", got.DoneAt)
	}
}

// TestTask_BeginWorkRestart_PreservesPlanPhase pins the bbolt-sourced
// reuse path: the existing plan-phase fields stay intact.
func TestTask_BeginWorkRestart_PreservesPlanPhase(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "seeded")
	dbPath, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := tasks.Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	prePlanBegin := existing.PlanBeginAt
	prePlanEnd := existing.PlanEndAt
	preCursor := existing.PlanResumeSession

	lc := BeginWorkRestart(existing, io.Discard, "cursor", "gpt-5", "fresh-work-cursor", "")
	lc.Finish(nil)

	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.PlanResumeSession != preCursor {
		t.Fatalf("PlanResumeSession changed: got %q, want %q", got.PlanResumeSession, preCursor)
	}
	if got.WorkResumeSession != "fresh-work-cursor" {
		t.Fatalf("WorkResumeSession = %q", got.WorkResumeSession)
	}
	if got.WorkModel != "gpt-5" {
		t.Fatalf("WorkModel = %q", got.WorkModel)
	}
	if got.PlanBeginAt.IsZero() || !got.PlanBeginAt.Equal(prePlanBegin) {
		t.Fatalf("PlanBeginAt = %v", got.PlanBeginAt)
	}
	if got.PlanEndAt.IsZero() || !got.PlanEndAt.Equal(prePlanEnd) {
		t.Fatalf("PlanEndAt = %v", got.PlanEndAt)
	}
}

// TestWorkLifecycle_FinishErrorPath drives the tasks.StatusHelp branch.
func TestWorkLifecycle_FinishErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewWorkTask(io.Discard, "cursor", "m", tasks.NewTaskID(), "/tmp/x.plan.md", "", "body", "", "")
	lc.Finish(errors.New("boom"))
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
	if !got.DoneAt.IsZero() {
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
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", tasks.NewTaskID(), "/tmp/x.plan.md", "", "body", "", "")
	lc.RecordBackground(54321, "/tmp/agent.log")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusWorking {
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
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", tasks.NewTaskID(), "/tmp/x.plan.md", "", "body", "", "")
	lc.Finish(nil)
	lc.RecordBackground(99999, "/tmp/should-not-stick.log")
	got := listAllTasks(t)[0]
	if got.Status != tasks.StatusWorkDone {
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
	lc := NewWorkTask(io.Discard, "cursor", "sonnet-4", tasks.NewTaskID(), "/tmp/x.plan.md", "", "body", "", "")
	lc.Finish(nil)
	lc.Finish(errors.New("ignored"))
	rows := listAllTasks(t)
	if len(rows) != 1 || rows[0].Status != tasks.StatusWorkDone {
		t.Fatalf("second finish should be a no-op: %+v", rows)
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
	path, err := tasks.DefaultDir()
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
	lc := NewWorkTask(&stderr, "cursor", "m", tasks.NewTaskID(), "/tmp/x.plan.md", "", "body", "", "")
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
	lc := &WorkLifecycle{stderr: &stderr, task: tasks.Task{Status: tasks.StatusWorking}}
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
	lc := NewWorkTask(io.Discard, "cursor", "m", tasks.NewTaskID(), "/tmp/x.plan.md", "# do the thing", "body", "", "")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "myproj-do-the-thing" {
		t.Fatalf("Worktree = %q", got.Worktree)
	}
}

// TestTask_BeginWorkRestart_MintsWorktreeWhenEmpty pins the
// reuse-mint-on-empty branch.
func TestTask_BeginWorkRestart_MintsWorktreeWhenEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "hello world")
	dbPath, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := tasks.Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if existing.Worktree != "" {
		t.Fatalf("seed already has worktree %q", existing.Worktree)
	}
	lc := BeginWorkRestart(existing, io.Discard, "cursor", "m", "cursor", "")
	lc.Finish(nil)
	got := listAllTasks(t)[0]
	if got.Worktree != "myproj-hello-world" {
		t.Fatalf("Worktree = %q", got.Worktree)
	}
}

// TestTask_BeginWorkRestart_PreservesPreExistingWorktree pins the
// preserve-existing-value branch of fillWorktree.
func TestTask_BeginWorkRestart_PreservesPreExistingWorktree(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := seedPlanDoneTask(t, "hello")
	dbPath, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	s := tasks.Open(dbPath)
	existing, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	existing.Worktree = "manual-override"
	lc := BeginWorkRestart(existing, io.Discard, "cursor", "m", "cursor", "")
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
	existing := tasks.Task{
		ID:     tasks.NewTaskID(),
		Status: tasks.StatusWorking,
		PlanTool: "cursor",
		WorkTool: "cursor",
		WorkModel: "sonnet-4",
		Summary: "hello",
	}
	tasks.PersistWarn(io.Discard, existing)
	if existing.Worktree != "" {
		t.Fatalf("created task already has worktree %q", existing.Worktree)
	}
	lc := BeginWorkResume(existing, io.Discard, "")
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
	lc := NewWorkTask(io.Discard, "cursor", "m", tasks.NewTaskID(), "/tmp/x.plan.md", "# do the thing", "body", "", "")
	if got := lc.Task(); got.Worktree != "myproj-do-the-thing" {
		t.Fatalf("Task().Worktree = %q", got.Worktree)
	}
}

// TestWorkLifecycle_MarkersGoToAgentLogNotStderr is the regression
// pin for "phase markers must never reach the user's terminal".
func TestWorkLifecycle_MarkersGoToAgentLogNotStderr(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "agent.log")
	var stderr bytes.Buffer
	t.Cleanup(tasks.ResetHooksForTest)
	tasks.Register(markersHook)
	lc := NewWorkTask(&stderr, "cursor", "m", tasks.NewTaskID(), "/tmp/x.plan.md", "", "body", "", logPath)
	lc.Finish(nil)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read agent.log: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "work begin") {
		t.Fatalf("agent.log missing work begin marker: %q", body)
	}
	if !strings.Contains(body, "work done") {
		t.Fatalf("agent.log missing work done marker: %q", body)
	}
	if strings.Contains(stderr.String(), agentlog.Header("work_begin")) {
		t.Fatalf("stderr leaked phase marker: %q", stderr.String())
	}
}
