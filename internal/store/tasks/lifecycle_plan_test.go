package tasks

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/agentlog"
)

// TestNewPlanTask_RecordsAndFinish drives the planning → plan-done
// happy path: NewPlanTask writes the row at status `planning`, then
// Finish stamps end_at and flips the row to plan-done. The summary
// uses the requirement body (first non-empty line) since it beats
// the file basename.
func TestNewPlanTask_RecordsAndFinish(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := NewTaskID()
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", id, "/tmp/x.md", "# heading\nbody", "plan-cursor", "", "")
	lc.Finish(nil, "# heading\nbody", "## plan", "/tmp/x.md")
	tasks := listAllTasks(t)
	if len(tasks) != 1 || tasks[0].ID != id {
		t.Fatalf("tasks = %+v", tasks)
	}
	got := tasks[0]
	if got.Status != StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.InvokedTool != "cursor" || got.InvokedModel != "sonnet-4" {
		t.Fatalf("tool/model = %q/%q", got.InvokedTool, got.InvokedModel)
	}
	if got.PlanResumeCursor != "plan-cursor" {
		t.Fatalf("PlanResumeCursor = %q", got.PlanResumeCursor)
	}
	if got.Summary != "heading" {
		t.Fatalf("Summary = %q, want heading", got.Summary)
	}
	if got.PlanBeginAt == nil || got.PlanEndAt == nil {
		t.Fatalf("timestamps missing: %+v", got)
	}
	if got.PlanEndAt.Before(*got.PlanBeginAt) {
		t.Fatalf("end %v before begin %v", got.PlanEndAt, got.PlanBeginAt)
	}
}

// TestPlanLifecycle_Finish_ErrorPath drives the StatusHelp branch
// when agent.Plan errored.
func TestPlanLifecycle_Finish_ErrorPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "m", NewTaskID(), "/tmp/x.md", "x", "", "", "")
	lc.Finish(errors.New("boom"), "", "", "/tmp/x.md")
	tasks := listAllTasks(t)
	if len(tasks) != 1 || tasks[0].Status != StatusHelp {
		t.Fatalf("tasks = %+v, want one help-status task", tasks)
	}
}

// TestPlanLifecycle_RecordBackground_StampsPIDAndPath drives the
// happy path of RecordBackground: the in-memory task row carries the
// PID and log path, status stays at planning, and a stray Finish call
// is a silent no-op thanks to the closed flag.
func TestPlanLifecycle_RecordBackground_StampsPIDAndPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.md", "# heading", "", "", "")
	lc.RecordBackground(99887, "/tmp/agent.log")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	got := listAllTasks(t)[0]
	if got.Status != StatusPlanning {
		t.Fatalf("Status = %q, want planning", got.Status)
	}
	if got.BackgroundPID != 99887 {
		t.Fatalf("BackgroundPID = %d", got.BackgroundPID)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
}

// TestPlanLifecycle_RecordBackground_ClosedShortCircuit pins the
// second-call no-op: once a lifecycle has been finalised, a
// subsequent RecordBackground does nothing.
func TestPlanLifecycle_RecordBackground_ClosedShortCircuit(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.md", "# heading", "", "", "")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	lc.RecordBackground(11111, "/tmp/should-not-stick.log")
	got := listAllTasks(t)[0]
	if got.Status != StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (closed branch)", got.BackgroundPID)
	}
	if got.AgentLogPath != "" {
		t.Fatalf("AgentLogPath = %q, want empty", got.AgentLogPath)
	}
}

// TestPlanLifecycle_FinishIdempotent pins the closed-flag short
// circuit so a second Finish call is a silent no-op.
func TestPlanLifecycle_FinishIdempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	lc := NewPlanTask(io.Discard, "cursor", "sonnet-4", NewTaskID(), "/tmp/x.md", "# heading", "", "", "")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")
	lc.Finish(errors.New("boom"), "should not", "change", "anything")
	tasks := listAllTasks(t)
	if len(tasks) != 1 || tasks[0].Status != StatusPlanDone {
		t.Fatalf("second finish should be a no-op: %+v", tasks)
	}
}

// TestPlanLifecycle_FinishPutErrorWarns drives the "tasks put"
// warning branch by feeding a task with no ID.
func TestPlanLifecycle_FinishPutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := &PlanLifecycle{stderr: &stderr, task: Task{Status: StatusPlanning}}
	lc.Finish(nil, "", "", "")
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestNewPlanTask_PutErrorAtBegin pins the put-error branch *inside*
// NewPlanTask: PutTask fails because the task has no ID, the warning
// surfaces, and the begin call still returns a usable lifecycle.
func TestNewPlanTask_PutErrorAtBegin(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	var stderr bytes.Buffer
	lc := NewPlanTask(&stderr, "cursor", "m", "", "", "", "", "", "")
	if lc == nil {
		t.Fatal("NewPlanTask returned nil")
	}
	t.Cleanup(func() { lc.Finish(nil, "", "", "") })
	if !strings.Contains(stderr.String(), "tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestNewPlanTask_OpenFails forces PutTask's mkdir of the per-task
// directory to fail by replacing `.j/tasks` with a regular file.
// Both NewPlanTask and Finish emit a warning and execution continues.
func TestNewPlanTask_OpenFails(t *testing.T) {
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
	lc := NewPlanTask(&stderr, "cursor", "m", NewTaskID(), "", "", "", "", "")
	if lc == nil {
		t.Fatal("NewPlanTask returned nil")
	}
	lc.Finish(nil, "", "", "")
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want some tasks warning", stderr.String())
	}
}

// TestPlanLifecycle_Task returns a value copy of the in-memory task
// row so callers can read it without poking at the unexported field.
func TestPlanLifecycle_Task(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	id := NewTaskID()
	lc := NewPlanTask(io.Discard, "cursor", "m", id, "", "", "", "", "")
	if got := lc.Task(); got.ID != id {
		t.Fatalf("Task().ID = %q, want %q", got.ID, id)
	}
}

// TestTask_BeginPlanReuse_PreservesLineage flips an existing plan-done
// row to planning, refreshes the plan resume cursor, and preserves
// the original PlanBeginAt while clearing PlanEndAt / DoneAt.
func TestTask_BeginPlanReuse_PreservesLineage(t *testing.T) {
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

	lc := existing.BeginPlanReuse(io.Discard, "cursor", "gpt-5", "fresh-plan-cursor", "")
	lc.Finish(nil, "# refined", "## plan", "/tmp/x.md")
	got := listAllTasks(t)[0]
	if got.Status != StatusPlanDone {
		t.Fatalf("Status = %q", got.Status)
	}
	if got.PlanResumeCursor != "fresh-plan-cursor" {
		t.Fatalf("PlanResumeCursor = %q", got.PlanResumeCursor)
	}
	if got.InvokedModel != "gpt-5" {
		t.Fatalf("InvokedModel = %q", got.InvokedModel)
	}
	if got.PlanBeginAt == nil || !got.PlanBeginAt.Equal(*prePlanBegin) {
		t.Fatalf("PlanBeginAt = %v, want %v", got.PlanBeginAt, prePlanBegin)
	}
	if got.Summary != "refined" {
		t.Fatalf("Summary = %q", got.Summary)
	}
}

// TestPlanLifecycle_MarkersGoToAgentLogNotStderr is the regression
// pin for "phase markers must never reach the user's terminal". The
// lifecycle is wired with a temp agent.log path; both markers must
// land in that file and stderr must stay clean of the agentlog
// sentinel.
func TestPlanLifecycle_MarkersGoToAgentLogNotStderr(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := store.EnsureProject(); err != nil {
		t.Fatalf("store.EnsureProject: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "agent.log")
	var stderr bytes.Buffer
	lc := NewPlanTask(&stderr, "cursor", "m", NewTaskID(), "/tmp/x.md", "# heading", "", logPath, "")
	lc.Finish(nil, "# heading", "plan", "/tmp/x.md")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read agent.log: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `"event":"phase_begin"`) {
		t.Fatalf("agent.log missing phase_begin: %q", body)
	}
	if !strings.Contains(body, `"event":"phase_end"`) {
		t.Fatalf("agent.log missing phase_end: %q", body)
	}
	if strings.Contains(stderr.String(), agentlog.Sentinel) {
		t.Fatalf("stderr leaked phase marker: %q", stderr.String())
	}
}
