package verifier

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/adk/session"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

func TestConstants(t *testing.T) {
	if Name != "verifier" {
		t.Fatalf("Name = %q", Name)
	}
	if OutputKey != "temp:review" {
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

// TestNew_ShellOutPassEscalates drives the shell-out happy path:
// scripted Verify writes `VERDICT: PASS` to verifier_findings.md →
// verify.Run finalises the row to `completed` → the shell-agent
// flips event.Actions.Escalate=true on the emitted summary event.
func TestNew_ShellOutPassEscalates(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(filepath.Join(taskDir, "plan.md"), "1. step"); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketVerifier, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:          taskID,
		Status:      tasks.StatusWorkDone,
		WorkTool:    "scripted",
		Summary:     "task",
	})

	stub := newScriptedVerifyAgent("scripted")
	stub.verdict = "VERDICT: PASS"
	a, err := New(Config{
		TaskID: taskID,
		Agents: []codingagents.Agent{stub},
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	events := testutil.DrainAgent(t, a)
	if stub.verifyCalls.Load() != 1 {
		t.Fatalf("Verify calls = %d, want 1", stub.verifyCalls.Load())
	}
	if !findEscalateEvent(events) {
		t.Fatalf("expected an event with Actions.Escalate=true; got %v", events)
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.Status != tasks.StatusCompleted {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
}

func TestNew_ShellOutFailDoesNotEscalate(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(filepath.Join(taskDir, "plan.md"), "1. step"); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketVerifier, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:          taskID,
		Status:      tasks.StatusWorkDone,
		WorkTool:    "scripted",
		Summary:     "task",
	})

	stub := newScriptedVerifyAgent("scripted")
	stub.verdict = "VERDICT: FAIL"
	a, err := New(Config{
		TaskID:        taskID,
		Agents:        []codingagents.Agent{stub},
		Stderr:        io.Discard,
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	events := testutil.DrainAgent(t, a)
	if findEscalateEvent(events) {
		t.Fatalf("Escalate must remain false on FAIL")
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.Status != tasks.StatusFailed {
		t.Fatalf("Status = %q, want failed", got.Status)
	}
}

func TestNew_ShellOutVerifyFails(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(filepath.Join(taskDir, "plan.md"), "1. step"); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketVerifier, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:          taskID,
		Status:      tasks.StatusWorkDone,
		WorkTool:    "scripted",
		Summary:     "task",
	})

	stub := newScriptedVerifyAgent("scripted")
	stub.verifyErr = errors.New("verify boom")
	a, err := New(Config{
		TaskID:        taskID,
		Agents:        []codingagents.Agent{stub},
		Stderr:        io.Discard,
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := testutil.DrainAgentForError(t, a); err == nil || !strings.Contains(err.Error(), "verify boom") {
		t.Fatalf("err = %v, want verify boom propagation", err)
	}
}

// TestNew_ShellOutDefaultsMaxIterations exercises the
// MaxIterations<=0 → defaultMaxIterations branch.
func TestNew_ShellOutDefaultsMaxIterations(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(filepath.Join(taskDir, "plan.md"), "1. step"); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketVerifier, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:          taskID,
		Status:      tasks.StatusWorkDone,
		WorkTool:    "scripted",
		Summary:     "task",
	})

	stub := newScriptedVerifyAgent("scripted")
	stub.verdict = "VERDICT: PASS"
	a, err := New(Config{TaskID: taskID, Agents: []codingagents.Agent{stub}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	testutil.DrainAgent(t, a)
	if stub.verifyCalls.Load() != 1 {
		t.Fatalf("Verify calls = %d", stub.verifyCalls.Load())
	}
}

// findEscalateEvent walks events in arrival order and returns true
// when at least one carries Actions.Escalate=true.
func findEscalateEvent(events []*session.Event) bool {
	for _, ev := range events {
		if ev != nil && ev.Actions.Escalate {
			return true
		}
	}
	return false
}

// scriptedVerifyAgent stands in for a real codingagents.Agent.
// Verify writes the configured verdict to the
// verifier_findings.md output path inline so verify.Run can parse
// it on the synchronous (pid==0) path.
type scriptedVerifyAgent struct {
	name        string
	models      []string
	verifyCalls atomic.Int32
	workCalls   atomic.Int32
	verdict     string
	verifyErr   error
}

func newScriptedVerifyAgent(name string) *scriptedVerifyAgent {
	return &scriptedVerifyAgent{name: name, models: []string{"m1"}}
}

func (a *scriptedVerifyAgent) Name() string                                 { return a.name }
func (a *scriptedVerifyAgent) ListModels(context.Context) ([]string, error) { return a.models, nil }
func (a *scriptedVerifyAgent) CheckLogin(context.Context) error             { return nil }
func (a *scriptedVerifyAgent) NewResumeID(context.Context) (string, error)  { return "rid", nil }

func (a *scriptedVerifyAgent) Plan(context.Context, codingagents.PlanRequest) (int, error) {
	return 0, errors.New("scriptedVerifyAgent.Plan should not be called")
}

func (a *scriptedVerifyAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	a.workCalls.Add(1)
	return 0, nil
}

func (a *scriptedVerifyAgent) Verify(_ context.Context, req codingagents.VerifyRequest) (int, error) {
	a.verifyCalls.Add(1)
	if a.verifyErr != nil {
		return 0, a.verifyErr
	}
	body := a.verdict
	if body == "" {
		body = "VERDICT: FAIL"
	}
	if err := testutil.WriteFile(req.VerifierFindingsOutputPath, body); err != nil {
		return 0, err
	}
	if err := testutil.WriteFile(req.VerifierPlanOutputPath, "verifier plan"); err != nil {
		return 0, err
	}
	return 0, nil
}
