package plan

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// readTasks opens the per-cwd tasks DB, lists every task, and closes
// the store. Tests call this after Run to assert the lifecycle wrote
// what we expect.
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

func TestRun_Markdown_LogsPlanDoneTask(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "# spec heading\nbody text")
	agent := newScriptedAgent()
	err := Run(context.Background(), Options{
		FromFile:    target,
		Interactive: true,
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1: %+v", len(tasks), tasks)
	}
	got := tasks[0]
	if got.Status != store.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.InvokedTool != "cursor" || got.InvokedModel != "sonnet-4" {
		t.Fatalf("tool/model = %q/%q", got.InvokedTool, got.InvokedModel)
	}
	if got.Summary != "spec heading" {
		t.Fatalf("Summary = %q, want %q", got.Summary, "spec heading")
	}
	if got.PlanBeginAt == nil || got.PlanEndAt == nil {
		t.Fatalf("timestamps should be set: begin=%v end=%v", got.PlanBeginAt, got.PlanEndAt)
	}
	if got.PlanEndAt.Before(*got.PlanBeginAt) {
		t.Fatalf("end %v before begin %v", got.PlanEndAt, got.PlanBeginAt)
	}
	if got.PlanResumeCursor != testCursorChatID {
		t.Fatalf("PlanResumeCursor = %q, want %q", got.PlanResumeCursor, testCursorChatID)
	}
	if got.WorkResumeCursor != "" || got.VerifyResumeCursor != "" {
		t.Fatalf("non-plan cursors should stay empty: work=%q verify=%q", got.WorkResumeCursor, got.VerifyResumeCursor)
	}
	tasksDir, err := store.DefaultTasksDir()
	if err != nil {
		t.Fatalf("DefaultTasksDir: %v", err)
	}
	requirementsPath := filepath.Join(tasksDir, got.ID, store.RequirementsFileName)
	if data, err := os.ReadFile(requirementsPath); err != nil {
		t.Fatalf("read requirements.md: %v", err)
	} else if !strings.Contains(string(data), "spec heading") {
		t.Fatalf("requirements.md = %q, want body", string(data))
	}
	planPath := filepath.Join(tasksDir, got.ID, store.PlanFileName)
	if data, err := os.ReadFile(planPath); err != nil {
		t.Fatalf("read plan.md: %v", err)
	} else if !strings.Contains(string(data), "step one") {
		t.Fatalf("plan.md = %q, want plan body", string(data))
	}
}

func TestRun_Markdown_AgentError_LogsHelpStatus(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	agent.planErr = errors.New("agent boom")
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusHelp {
		t.Fatalf("tasks = %+v, want one help-status task", tasks)
	}
}

// TestRun_Markdown_AgentSkippedWrite pins the branch where the agent
// returned success but did not produce either output file. The task is
// still recorded as plan-done; warnings surface for both reads. The
// task summary falls back to the source file basename because the
// requirements body could not be re-read.
func TestRun_Markdown_AgentSkippedWrite(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	target := writeFromFile(t, "x")
	agent := newScriptedAgent()
	agent.skipWrite = true
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		FromFile: target,
		Stdout:   io.Discard,
		Stderr:   &stderr,
		Agents:   []codingagents.Agent{agent},
		UI:       &scriptedUI{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d", len(tasks))
	}
	if tasks[0].Status != store.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", tasks[0].Status)
	}
	if tasks[0].Summary != "spec.md" {
		t.Fatalf("Summary = %q, want fallback to basename", tasks[0].Summary)
	}
}

// TestRecordBackground_StampsPIDAndPath drives the happy path of
// recordBackground: the in-memory task row carries the PID and log
// path, status stays at planning (foreground would have transitioned
// it to plan-done), and a stray finishPlan call is a silent no-op
// thanks to the closed flag.
func TestRecordBackground_StampsPIDAndPath(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	taskID := store.NewTaskID()
	lc := beginPlanTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", taskID, "/tmp/x.md", "# heading", "")
	lc.recordBackground(99887, "/tmp/agent.log")
	lc.finishPlan(nil, "# heading", "plan", "/tmp/x.md")
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d", len(tasks))
	}
	got := tasks[0]
	if got.Status != store.StatusPlanning {
		t.Fatalf("Status = %q, want planning (recordBackground must not flip)", got.Status)
	}
	if got.BackgroundPID != 99887 {
		t.Fatalf("BackgroundPID = %d", got.BackgroundPID)
	}
	if got.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q", got.AgentLogPath)
	}
}

// TestRecordBackground_ClosedShortCircuit pins the second-call
// no-op: once a lifecycle has been finalised (closed flag set), a
// subsequent recordBackground does nothing — no PID stamp, no
// re-persist.
func TestRecordBackground_ClosedShortCircuit(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	taskID := store.NewTaskID()
	lc := beginPlanTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", taskID, "/tmp/x.md", "# heading", "")
	lc.finishPlan(nil, "# heading", "plan", "/tmp/x.md")
	lc.recordBackground(11111, "/tmp/should-not-stick.log")
	tasks := readTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d", len(tasks))
	}
	got := tasks[0]
	if got.Status != store.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
	if got.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (closed branch)", got.BackgroundPID)
	}
	if got.AgentLogPath != "" {
		t.Fatalf("AgentLogPath = %q, want empty", got.AgentLogPath)
	}
}

// TestFinishPlan_Idempotent pins the closed-flag short-circuit so a
// second finishPlan call is a silent no-op.
func TestFinishPlan_Idempotent(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	lc := beginPlanTask(Options{Stderr: io.Discard}, &scriptedAgent{name: "cursor"}, "sonnet-4", store.NewTaskID(), "/tmp/x.md", "# heading", "")
	lc.finishPlan(nil, "# heading", "plan", "/tmp/x.md")
	lc.finishPlan(errors.New("boom"), "should not", "change", "anything")
	tasks := readTasks(t)
	if len(tasks) != 1 || tasks[0].Status != store.StatusPlanDone {
		t.Fatalf("second finish should be a no-op: %+v", tasks)
	}
}

// TestFinishPlan_PutErrorWarns drives the "tasks put" warning branch
// inside finishPlan by feeding a task with no ID through the
// lifecycle. tasklog.OpenTaskLog succeeds (the .j layout is
// initialised), but store.PutTask rejects the empty ID, so
// tasklog.PersistWarn emits the expected warning.
func TestFinishPlan_PutErrorWarns(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	var stderr bytes.Buffer
	lc := &planLifecycle{stderr: &stderr, task: store.Task{
		Status: store.StatusPlanning,
	}}
	lc.finishPlan(nil, "", "", "")
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestBeginPlanTask_PutErrorAtBegin pins the put-error branch *inside*
// beginPlanTask: the store opens but PutTask fails because the task
// has no ID. The warning surfaces on stderr and the begin call still
// returns a usable lifecycle.
func TestBeginPlanTask_PutErrorAtBegin(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	if _, err := store.EnsureTaskDir("seed"); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	lc := beginPlanTask(Options{Stderr: &stderr}, &scriptedAgent{name: "cursor"}, "m", "", "", "", "")
	if lc == nil {
		t.Fatal("beginPlanTask returned nil lifecycle")
	}
	t.Cleanup(func() { lc.finishPlan(nil, "", "", "") })
	if !strings.Contains(stderr.String(), "warning: tasks put") {
		t.Fatalf("stderr = %q, want tasks-put warning", stderr.String())
	}
}

// TestBeginPlanTask_OpenTaskLogFails forces openTaskLog to return
// ok=false by replacing the post-init list.db file with a directory,
// so bolt.Open errors. Both beginPlanTask and finishPlan emit the
// expected warning and execution continues without panicking.
func TestBeginPlanTask_OpenTaskLogFails(t *testing.T) {
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
	lc := beginPlanTask(Options{Stderr: &stderr}, &scriptedAgent{name: "cursor"}, "m", store.NewTaskID(), "", "", "")
	if lc == nil {
		t.Fatal("beginPlanTask returned nil lifecycle")
	}
	lc.finishPlan(nil, "", "", "")
	if !strings.Contains(stderr.String(), "tasks") {
		t.Fatalf("stderr = %q, want some tasks warning", stderr.String())
	}
}

// helper is unused but kept for clarity that t.Chdir can be combined
// with explicit context propagation in tests.
var _ = context.Background
