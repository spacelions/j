package worker

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

func TestConstants(t *testing.T) {
	if Name != "worker" {
		t.Fatalf("Name = %q", Name)
	}
	if OutputKey != "code" {
		t.Fatalf("OutputKey = %q", OutputKey)
	}
}

func TestNew_LLMBranch(t *testing.T) {
	a, err := New(Config{LLM: testutil.StubModel{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a == nil {
		t.Fatal("agent is nil")
	}
	if a.Name() != Name {
		t.Fatalf("Name() = %q", a.Name())
	}
}

func TestNew_BothBranchesSetIsError(t *testing.T) {
	_, err := New(Config{LLM: testutil.StubModel{}, TaskID: "t1"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v, want mutually-exclusive guard", err)
	}
}

func TestNew_NeitherBranchIsError(t *testing.T) {
	_, err := New(Config{})
	if err == nil || !strings.Contains(err.Error(), "LLM or TaskID") {
		t.Fatalf("err = %v, want LLM-or-TaskID guard", err)
	}
}

func TestNew_ShellOutMissingAgents(t *testing.T) {
	_, err := New(Config{TaskID: "t1"})
	if err == nil || !strings.Contains(err.Error(), "Agents") {
		t.Fatalf("err = %v, want Agents guard", err)
	}
}

// TestNew_ShellOutHappyPath drives the shell-out branch end to end:
// New(Config{TaskID, Agents}) → runner.Run → work.Run → scripted
// Work → finishWork promotes the row to work-done.
func TestNew_ShellOutHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/plan.md", "1. step"); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketWorker, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:          taskID,
		Status:      tasks.StatusPlanDone,
		PlanTool:    "scripted",
		Summary:     "task",
	})

	stub := newScriptedWorkAgent("scripted")
	a, err := New(Config{
		TaskID: taskID,
		Agents: []codingagents.Agent{stub},
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	events := testutil.DrainAgent(t, a)
	if stub.workCalls != 1 {
		t.Fatalf("Work calls = %d, want 1", stub.workCalls)
	}
	if len(events) == 0 {
		t.Fatalf("expected at least one phase event")
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", got.Status)
	}
}

func TestNew_ShellOutWorkFails(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/plan.md", "1. step"); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketWorker, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:          taskID,
		Status:      tasks.StatusPlanDone,
		PlanTool:    "scripted",
		Summary:     "task",
	})

	stub := newScriptedWorkAgent("scripted")
	stub.workErr = errors.New("worker boom")
	a, err := New(Config{
		TaskID: taskID,
		Agents: []codingagents.Agent{stub},
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := testutil.DrainAgentForError(t, a); err == nil || !strings.Contains(err.Error(), "worker boom") {
		t.Fatalf("err = %v, want worker boom propagation", err)
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
}

func TestNew_ShellOutDefaultsStderr(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/plan.md", "1. step"); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketWorker, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:          taskID,
		Status:      tasks.StatusPlanDone,
		PlanTool:    "scripted",
		Summary:     "task",
	})

	stub := newScriptedWorkAgent("scripted")
	a, err := New(Config{TaskID: taskID, Agents: []codingagents.Agent{stub}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testutil.DrainAgent(t, a)
	if stub.workCalls != 1 {
		t.Fatalf("Work calls = %d, want 1", stub.workCalls)
	}
}

// scriptedWorkAgent stands in for a real codingagents.Agent. Work
// completes inline (no spawned child) so work.Run's finishWork
// promotes the row to work-done synchronously.
type scriptedWorkAgent struct {
	name      string
	models    []string
	workCalls int
	workErr   error
}

func newScriptedWorkAgent(name string) *scriptedWorkAgent {
	return &scriptedWorkAgent{name: name, models: []string{"m1"}}
}

func (a *scriptedWorkAgent) Name() string                                 { return a.name }
func (a *scriptedWorkAgent) ListModels(context.Context) ([]string, error) { return a.models, nil }
func (a *scriptedWorkAgent) CheckLogin(context.Context) error             { return nil }
func (a *scriptedWorkAgent) NewResumeID(context.Context) (string, error)  { return "rid", nil }

func (a *scriptedWorkAgent) Plan(context.Context, codingagents.PlanRequest) (int, error) {
	return 0, errors.New("scriptedWorkAgent.Plan should not be called")
}

func (a *scriptedWorkAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	a.workCalls++
	return 0, a.workErr
}

func (a *scriptedWorkAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("scriptedWorkAgent.Verify should not be called")
}
