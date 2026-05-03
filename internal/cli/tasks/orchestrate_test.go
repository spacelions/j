package tasks

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
)

// chainAgent stands in for a real codingagents.Agent across the
// planner / worker / verifier shell-out branches that
// RunOrchestrate drives via workflow.RunForTask. Plan / Work /
// Verify each complete inline so the synchronous lifecycle paths
// run end to end without a real subprocess.
type chainAgent struct {
	name        string
	models      []string
	planCalls   atomic.Int32
	workCalls   atomic.Int32
	verifyCalls atomic.Int32
	verdicts    []string // sequence consumed in order; last value sticks.
	planErr     error
}

func newChainAgent(name string) *chainAgent {
	return &chainAgent{name: name, models: []string{"m1"}}
}

func (a *chainAgent) Name() string                                 { return a.name }
func (a *chainAgent) ListModels(context.Context) ([]string, error) { return a.models, nil }
func (a *chainAgent) CheckLogin(context.Context) error             { return nil }
func (a *chainAgent) NewResumeID(context.Context) (string, error)  { return "rid", nil }

func (a *chainAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.planCalls.Add(1)
	if a.planErr != nil {
		return 0, a.planErr
	}
	if err := os.WriteFile(req.RequirementsOutputPath, []byte("# task\nbody"), 0o644); err != nil {
		return 0, err
	}
	if err := os.WriteFile(req.PlanOutputPath, []byte("1. step"), 0o644); err != nil {
		return 0, err
	}
	return 0, nil
}

func (a *chainAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	a.workCalls.Add(1)
	return 0, nil
}

func (a *chainAgent) Verify(_ context.Context, req codingagents.VerifyRequest) (int, error) {
	idx := a.verifyCalls.Add(1) - 1
	verdict := "VERDICT: FAIL"
	if int(idx) < len(a.verdicts) {
		verdict = a.verdicts[idx]
	} else if len(a.verdicts) > 0 {
		verdict = a.verdicts[len(a.verdicts)-1]
	}
	if err := os.WriteFile(req.VerifierFindingsOutputPath, []byte(verdict), 0o644); err != nil {
		return 0, err
	}
	if err := os.WriteFile(req.VerifierPlanOutputPath, []byte("verifier plan"), 0o644); err != nil {
		return 0, err
	}
	return 0, nil
}

// seedOrchestrateTask seeds a planning row + per-task dir + every
// agent bucket so RunOrchestrate's downstream plan / work / verify
// shell-out branches see the inputs they expect.
func seedOrchestrateTask(t *testing.T, tool string) string {
	t.Helper()
	id := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(id)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, store.RequirementsFileName), []byte("# task\nbody"), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, tool, "m1")
		writeBucketKey(t, bucket, "interactive", "false")
	}
	row := store.Task{
		ID:          id,
		Status:      store.StatusPlanning,
		InvokedTool: tool,
		Summary:     "task",
	}
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(row); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
	return id
}

// writeBucketKey is a single-line writer used by the orchestrate
// tests to set the per-bucket interactive flag (matters because
// the shell-out branches force interactive=false but we want to
// exercise the read path too).
func writeBucketKey(t *testing.T, bucket, key, value string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Put(bucket, key, value); err != nil {
		t.Fatalf("Put %s.%s: %v", bucket, key, err)
	}
}

func readOrchestrateTaskRow(t *testing.T, id string) store.Task {
	t.Helper()
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return got
}

// TestRunOrchestrate_RequiresTaskID pins the empty-id guard.
func TestRunOrchestrate_RequiresTaskID(t *testing.T) {
	err := RunOrchestrate(context.Background(), OrchestrateOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newChainAgent("scripted")},
	})
	if err == nil || !strings.Contains(err.Error(), "--id") {
		t.Fatalf("err = %v, want --id guard", err)
	}
}

// TestRunOrchestrate_RequiresAgents pins the no-agents guard.
func TestRunOrchestrate_RequiresAgents(t *testing.T) {
	err := RunOrchestrate(context.Background(), OrchestrateOptions{
		TaskID: "t1",
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunOrchestrate_PassFirstTry pins the happy path: planner →
// worker → verifier all succeed; verifier writes VERDICT: PASS;
// the row reaches `completed`.
func TestRunOrchestrate_PassFirstTry(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")
	stub.verdicts = []string{"VERDICT: PASS"}

	if err := RunOrchestrate(context.Background(), OrchestrateOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	row := readOrchestrateTaskRow(t, id)
	if row.Status != store.StatusCompleted {
		t.Fatalf("Status = %q, want completed", row.Status)
	}
	if stub.planCalls.Load() != 1 {
		t.Fatalf("plan calls = %d, want 1", stub.planCalls.Load())
	}
	if stub.workCalls.Load() != 1 {
		t.Fatalf("work calls = %d, want 1", stub.workCalls.Load())
	}
	if stub.verifyCalls.Load() != 1 {
		t.Fatalf("verify calls = %d, want 1", stub.verifyCalls.Load())
	}
}

// TestRunOrchestrate_FailRetryPass drives the retry path:
// MaxIterations=2, verifier returns FAIL on iteration 1 and PASS on
// iteration 2. The fix loop inside verify.Run runs the worker once
// in between. Final state: completed.
func TestRunOrchestrate_FailRetryPass(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	putProjectMaxIters(t, "2")
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")
	stub.verdicts = []string{"VERDICT: FAIL", "VERDICT: PASS"}

	if err := RunOrchestrate(context.Background(), OrchestrateOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	row := readOrchestrateTaskRow(t, id)
	if row.Status != store.StatusCompleted {
		t.Fatalf("Status = %q, want completed", row.Status)
	}
	if stub.verifyCalls.Load() != 2 {
		t.Fatalf("verify calls = %d, want 2 (FAIL retry then PASS)", stub.verifyCalls.Load())
	}
	// Worker shell-out + verify-loop fix worker = 2 work calls.
	if stub.workCalls.Load() < 2 {
		t.Fatalf("work calls = %d, want >=2 (initial worker + fix worker)", stub.workCalls.Load())
	}
}

// TestRunOrchestrate_FailExhausts pins the exhausted-retries
// branch: MaxIterations=1, verifier returns FAIL → `verify-done`.
func TestRunOrchestrate_FailExhausts(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	putProjectMaxIters(t, "1")
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")
	stub.verdicts = []string{"VERDICT: FAIL"}

	if err := RunOrchestrate(context.Background(), OrchestrateOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	row := readOrchestrateTaskRow(t, id)
	if row.Status != store.StatusVerifyDone {
		t.Fatalf("Status = %q, want verify-done", row.Status)
	}
}

// TestRunOrchestrate_PlanFailsHelp pins the planner-failure path:
// the row ends as `help` and worker / verifier never fire.
func TestRunOrchestrate_PlanFailsHelp(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")
	stub.planErr = errors.New("planning boom")

	err := RunOrchestrate(context.Background(), OrchestrateOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
	})
	if err == nil || !strings.Contains(err.Error(), "planning boom") {
		t.Fatalf("err = %v, want planning boom propagation", err)
	}
	if stub.workCalls.Load() != 0 || stub.verifyCalls.Load() != 0 {
		t.Fatalf("worker / verifier should not fire after plan failure")
	}
	row := readOrchestrateTaskRow(t, id)
	if row.Status != store.StatusHelp {
		t.Fatalf("Status = %q, want help", row.Status)
	}
}

// TestRunOrchestrate_AppliesDefaults exercises the nil stdin /
// stdout / stderr defaults via withDefaults. The run path is the
// happy path.
func TestRunOrchestrate_AppliesDefaults(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")
	stub.verdicts = []string{"VERDICT: PASS"}

	if err := RunOrchestrate(context.Background(), OrchestrateOptions{
		TaskID: id,
		Agents: []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
}

// TestNewOrchestrateCmd_FlagDefaults pins the flag surface.
func TestNewOrchestrateCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newOrchestrateCmd()
	if cmd.Use != "orchestrate" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	if !cmd.Hidden {
		t.Fatalf("orchestrate cmd should be Hidden")
	}
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	if len(names) != 1 || names[0] != "id" {
		t.Fatalf("flags = %v, want only [id]", names)
	}
}

// TestNewOrchestrateCmd_FlagsBindToViper covers --id viper binding.
func TestNewOrchestrateCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newOrchestrateCmd()
	if err := cmd.Flags().Set("id", "01ABC"); err != nil {
		t.Fatalf("Flags().Set id: %v", err)
	}
	if got := viper.GetString("tasks.orchestrate.id"); got != "01ABC" {
		t.Errorf("tasks.orchestrate.id = %q", got)
	}
}

// TestNewOrchestrateCmd_EnvBindings covers TASKS_ORCHESTRATE_ID.
func TestNewOrchestrateCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_ORCHESTRATE_ID", "01ENV")
	_ = newOrchestrateCmd()
	if got := viper.GetString("tasks.orchestrate.id"); got != "01ENV" {
		t.Errorf("tasks.orchestrate.id = %q", got)
	}
}

// TestNewOrchestrateCmd_RunE_PropagatesError exercises the RunE
// closure: an empty --id surfaces the wrapped error from
// RunOrchestrate.
func TestNewOrchestrateCmd_RunE_PropagatesError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	mustInit(t)
	cmd := newOrchestrateCmd()
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("expected an error from missing --id")
	}
}

// TestRunOrchestrate_RegisteredAsChild verifies orchestrate is a
// hidden cobra child of `j tasks`.
func TestRunOrchestrate_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "orchestrate" {
			if !sub.Hidden {
				t.Fatal("orchestrate should be Hidden on `j tasks`")
			}
			return
		}
	}
	t.Fatal("`j tasks orchestrate` should be registered as a child of `j tasks`")
}

// putProjectMaxIters writes project.max_iterations so
// store.LoadTaskConfig picks up the supplied bound.
func putProjectMaxIters(t *testing.T, value string) {
	t.Helper()
	path, err := store.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Put(store.BucketProject, "max_iterations", value); err != nil {
		t.Fatalf("Put max_iterations: %v", err)
	}
}
