package planner

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

func TestConstants(t *testing.T) {
	if Name != "planner" {
		t.Fatalf("Name = %q", Name)
	}
	if OutputKey != "plan" {
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
// New(Config{TaskID, Agents}) → runner.Run → plan.Run → scripted
// Plan executes inline → finishPlan promotes the row to plan-done.
func TestNew_ShellOutHappyPath(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "# task\nbody"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, store.Task{ID: taskID, Status: store.StatusPlanning, Summary: "task"})

	stub := newScriptedPlanAgent("scripted")
	a, err := New(Config{
		TaskID: taskID,
		Agents: []codingagents.Agent{stub},
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	events := testutil.DrainAgent(t, a)
	if stub.planCalls != 1 {
		t.Fatalf("Plan calls = %d, want 1", stub.planCalls)
	}
	if len(events) == 0 {
		t.Fatalf("expected at least one phase event")
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.Status != store.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
}

func TestNew_ShellOutPlanFails(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "x"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, store.Task{ID: taskID, Status: store.StatusPlanning, Summary: "task"})

	stub := newScriptedPlanAgent("scripted")
	stub.planErr = errors.New("planning boom")
	a, err := New(Config{
		TaskID: taskID,
		Agents: []codingagents.Agent{stub},
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := testutil.DrainAgentForError(t, a); err == nil || !strings.Contains(err.Error(), "planning boom") {
		t.Fatalf("err = %v, want planning boom propagation", err)
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.Status != store.StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
}

func TestNew_ShellOutDefaultsStderr(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "y"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, store.Task{ID: taskID, Status: store.StatusPlanning, Summary: "task"})

	stub := newScriptedPlanAgent("scripted")
	a, err := New(Config{TaskID: taskID, Agents: []codingagents.Agent{stub}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testutil.DrainAgent(t, a)
	if stub.planCalls != 1 {
		t.Fatalf("Plan calls = %d, want 1", stub.planCalls)
	}
}

// scriptedPlanAgent stands in for a real codingagents.Agent. Plan
// writes the per-task requirements.md / plan.md inline so plan.Run's
// finishPlan promotes the row to plan-done synchronously.
type scriptedPlanAgent struct {
	name      string
	models    []string
	planCalls int
	planErr   error
}

func newScriptedPlanAgent(name string) *scriptedPlanAgent {
	return &scriptedPlanAgent{name: name, models: []string{"m1"}}
}

func (a *scriptedPlanAgent) Name() string                                 { return a.name }
func (a *scriptedPlanAgent) ListModels(context.Context) ([]string, error) { return a.models, nil }
func (a *scriptedPlanAgent) CheckLogin(context.Context) error             { return nil }
func (a *scriptedPlanAgent) NewResumeID(context.Context) (string, error)  { return "rid", nil }

func (a *scriptedPlanAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.planCalls++
	if a.planErr != nil {
		return 0, a.planErr
	}
	if err := testutil.WriteFile(req.RequirementsOutputPath, "plan-refined-requirements"); err != nil {
		return 0, err
	}
	if err := testutil.WriteFile(req.PlanOutputPath, "1. step"); err != nil {
		return 0, err
	}
	return 0, nil
}

func (a *scriptedPlanAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, errors.New("scriptedPlanAgent.Work should not be called")
}
func (a *scriptedPlanAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("scriptedPlanAgent.Verify should not be called")
}
