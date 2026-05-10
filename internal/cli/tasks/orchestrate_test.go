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
	"github.com/spacelions/j/internal/lifecycle/orchestrator"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// chainAgent stands in for a real codingagents.Agent across the
// planner / worker / verifier shell-out branches that
// RunOrchestrate drives via orchestrator.RunForTask. Plan / Work /
// Verify each complete inline so the synchronous lifecycle paths
// run end to end without a real subprocess.
type chainAgent struct {
	name              string
	models            []string
	planCalls         atomic.Int32
	workCalls         atomic.Int32
	verifyCalls       atomic.Int32
	verdicts          []string // sequence consumed in order; last value sticks.
	planErr           error
	lastPlanReq       codingagents.PlanRequest
	newResumeIDCalls  atomic.Int32
	newResumeIDPanics bool
}

func newChainAgent(name string) *chainAgent {
	return &chainAgent{name: name, models: []string{"m1"}}
}

func (a *chainAgent) Name() string                                 { return a.name }
func (a *chainAgent) ListModels(context.Context) ([]string, error) { return a.models, nil }
func (a *chainAgent) CheckLogin(context.Context) error             { return nil }
func (a *chainAgent) NewResumeID(context.Context) (string, error) {
	a.newResumeIDCalls.Add(1)
	if a.newResumeIDPanics {
		panic("NewResumeID must not be called on resume runs")
	}
	return "rid", nil
}

func (a *chainAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.planCalls.Add(1)
	a.lastPlanReq = req
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

func (*chainAgent) FormatLog(line []byte) []byte { return line }

// seedOrchestrateTask seeds a planning row + per-task dir + every
// agent bucket so RunOrchestrate's downstream plan / work / verify
// shell-out branches see the inputs they expect.
func seedOrchestrateTask(t *testing.T, tool string) string {
	t.Helper()
	id := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, tasks.RequirementsFileName), []byte("# task\nbody"), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		writeBucketKey(t, store.BucketProject, store.KeyPlanRequiresApproval, "false")
		testutil.SeedAgentBucketToolModel(t, bucket, tool, "m1")
		writeBucketKey(t, bucket, "interactive", "false")
	}
	row := tasks.Task{
		ID:       id,
		Status:   tasks.StatusPlanning,
		PlanTool: tool,
		Summary:  "task",
	}
	s, err := tasks.OpenDefault()
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

func readOrchestrateTaskRow(t *testing.T, id string) tasks.Task {
	t.Helper()
	s, err := tasks.OpenDefault()
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

func noPlanApproval() *bool {
	v := false
	return &v
}

func requirePlanApproval() *bool {
	v := true
	return &v
}

// TestRunOrchestrate_RequiresTaskID pins the empty-id guard.
func TestRunOrchestrate_RequiresTaskID(t *testing.T) {
	err := RunOrchestrate(t.Context(), OrchestrateOptions{
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
	err := RunOrchestrate(t.Context(), OrchestrateOptions{
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

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: noPlanApproval(),
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	row := readOrchestrateTaskRow(t, id)
	if row.Status != tasks.StatusCompleted {
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

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: noPlanApproval(),
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	row := readOrchestrateTaskRow(t, id)
	if row.Status != tasks.StatusCompleted {
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
// branch: MaxIterations=1, verifier returns FAIL → `failed`.
func TestRunOrchestrate_FailExhausts(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	putProjectMaxIters(t, "1")
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")
	stub.verdicts = []string{"VERDICT: FAIL"}

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: noPlanApproval(),
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	row := readOrchestrateTaskRow(t, id)
	if row.Status != tasks.StatusFailed {
		t.Fatalf("Status = %q, want failed", row.Status)
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

	err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: noPlanApproval(),
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	})
	if err == nil || !strings.Contains(err.Error(), "planning boom") {
		t.Fatalf("err = %v, want planning boom propagation", err)
	}
	if stub.workCalls.Load() != 0 || stub.verifyCalls.Load() != 0 {
		t.Fatalf("worker / verifier should not fire after plan failure")
	}
	row := readOrchestrateTaskRow(t, id)
	if row.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", row.Status)
	}
}

// TestRunOrchestrate_PlanApprovalStopsAfterPlan pins the approval
// gate: when enabled, the detached child runs planner only and leaves
// the row at plan-done for `j tasks continue`.
func TestRunOrchestrate_PlanApprovalStopsAfterPlan(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: requirePlanApproval(),
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	row := readOrchestrateTaskRow(t, id)
	if row.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", row.Status)
	}
	if stub.planCalls.Load() != 1 {
		t.Fatalf("plan calls = %d, want 1", stub.planCalls.Load())
	}
	if stub.workCalls.Load() != 0 || stub.verifyCalls.Load() != 0 {
		t.Fatalf("worker/verifier should not run with approval gate: work=%d verify=%d",
			stub.workCalls.Load(), stub.verifyCalls.Load())
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

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
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
	// pflag visits flags in lexicographic order.
	want := []string{"id", "interactive", "model", "phase", "plan-requires-approval", "tool", "yes"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("flags = %v, want %v", names, want)
	}
}

// TestNewOrchestrateCmd_FlagsBindToViper covers --id and
// --plan-requires-approval viper bindings.
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
	if err := cmd.Flags().Set("plan-requires-approval", "true"); err != nil {
		t.Fatalf("Flags().Set plan-requires-approval: %v", err)
	}
	if got := viper.GetBool("tasks.orchestrate.plan_requires_approval"); !got {
		t.Errorf("tasks.orchestrate.plan_requires_approval = false, want true")
	}
}

// TestNewOrchestrateCmd_EnvBindings covers TASKS_ORCHESTRATE_ID and
// TASKS_ORCHESTRATE_PLAN_REQUIRES_APPROVAL.
func TestNewOrchestrateCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_ORCHESTRATE_ID", "01ENV")
	t.Setenv("TASKS_ORCHESTRATE_PLAN_REQUIRES_APPROVAL", "true")
	_ = newOrchestrateCmd()
	if got := viper.GetString("tasks.orchestrate.id"); got != "01ENV" {
		t.Errorf("tasks.orchestrate.id = %q", got)
	}
	if got := viper.GetBool("tasks.orchestrate.plan_requires_approval"); !got {
		t.Errorf("tasks.orchestrate.plan_requires_approval = false, want true")
	}
}

func TestOrchestratePlanRequiresApprovalOverride_NoFlag(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newOrchestrateCmd()
	got, err := orchestratePlanRequiresApprovalOverride(cmd)
	if err != nil {
		t.Fatalf("orchestratePlanRequiresApprovalOverride: %v", err)
	}
	if got != nil {
		t.Fatalf("override = %v, want nil", *got)
	}
}

// TestNewOrchestrateCmd_PhaseFlagBindings covers --phase viper +
// env bindings — the single phase flag replaces the prior
// --skip-planning / --skip-work bool pair.
func TestNewOrchestrateCmd_PhaseFlagBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newOrchestrateCmd()
	if err := cmd.Flags().Set("phase", "from-work"); err != nil {
		t.Fatalf("Flags().Set phase: %v", err)
	}
	if got := viper.GetString("tasks.orchestrate.phase"); got != "from-work" {
		t.Errorf("tasks.orchestrate.phase = %q, want from-work", got)
	}
}

func TestNewOrchestrateCmd_PhaseEnvBinding(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_ORCHESTRATE_PHASE", "verify-only")
	_ = newOrchestrateCmd()
	if got := viper.GetString("tasks.orchestrate.phase"); got != "verify-only" {
		t.Errorf("tasks.orchestrate.phase = %q, want verify-only", got)
	}
}

func TestNewOrchestrateCmd_RejectsUnknownPhase(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newOrchestrateCmd()
	cmd.SetArgs([]string{"--phase", "worker"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute succeeded, want phase parse error")
	}
	if !strings.Contains(err.Error(), "unknown run phase") {
		t.Fatalf("error = %v", err)
	}
}

// TestRunOrchestrate_FromWorkRunsWorkVerify pins that Phase=FromWork
// drives only worker → verifier without re-running the planner. The
// seeded task already has plan.md staged because the planner phase
// is skipped.
func TestRunOrchestrate_FromWorkRunsWorkVerify(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, id, tasks.PlanFileName), []byte("1. step"), 0o644); err != nil {
		t.Fatal(err)
	}
	stub := newChainAgent("scripted")
	stub.verdicts = []string{"VERDICT: PASS"}

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: noPlanApproval(),
		Phase:                orchestrator.RunPhaseFromWork,
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	if stub.planCalls.Load() != 0 {
		t.Fatalf("plan calls = %d, want 0 (from-work must not re-plan)", stub.planCalls.Load())
	}
	if stub.workCalls.Load() != 1 || stub.verifyCalls.Load() != 1 {
		t.Fatalf("call counts: work=%d verify=%d", stub.workCalls.Load(), stub.verifyCalls.Load())
	}
}

func TestRunOrchestrate_PlanOnlyRunsPlanner(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID: id,
		Phase:  orchestrator.RunPhasePlanOnly,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	if stub.planCalls.Load() != 1 ||
		stub.workCalls.Load() != 0 ||
		stub.verifyCalls.Load() != 0 {
		t.Fatalf("call counts: plan=%d work=%d verify=%d",
			stub.planCalls.Load(),
			stub.workCalls.Load(),
			stub.verifyCalls.Load())
	}
}

func TestRunOrchestrate_WorkOnlyRunsWorker(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	stagePlan(t, id)
	stub := newChainAgent("scripted")

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID: id,
		Phase:  orchestrator.RunPhaseWorkOnly,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	if stub.planCalls.Load() != 0 ||
		stub.workCalls.Load() != 1 ||
		stub.verifyCalls.Load() != 0 {
		t.Fatalf("call counts: plan=%d work=%d verify=%d",
			stub.planCalls.Load(),
			stub.workCalls.Load(),
			stub.verifyCalls.Load())
	}
}

func TestRunOrchestrate_RejectsUnknownPhase(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID: id,
		Phase:  "bogus",
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newChainAgent("scripted")},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown phase "bogus"`) {
		t.Fatalf("err = %v, want unknown phase", err)
	}
}

func TestDispatchNonFullRejectsFullPhase(t *testing.T) {
	err := dispatchNonFullOrchestratePhase(
		t.Context(),
		orchestrator.TaskContext{},
		orchestrator.PhaseConfig{Phase: orchestrator.RunPhaseFull},
	)
	if err == nil || !strings.Contains(err.Error(), `unknown phase "full"`) {
		t.Fatalf("err = %v, want full phase rejection", err)
	}
}

// TestRunOrchestrate_FromWorkConflictsWithApproval pins the rejected
// combination: --phase=from-work with --plan-requires-approval=true
// must error before invoking the orchestrator.
func TestRunOrchestrate_FromWorkConflictsWithApproval(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")

	err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: requirePlanApproval(),
		Phase:                orchestrator.RunPhaseFromWork,
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	})
	if err == nil || !strings.Contains(err.Error(), "from-work") {
		t.Fatalf("err = %v, want from-work incompatibility guard", err)
	}
	if stub.planCalls.Load()+stub.workCalls.Load()+stub.verifyCalls.Load() != 0 {
		t.Fatalf("no agent should run when conflict guard fires")
	}
}

func TestOrchestratePlanRequiresApprovalOverride_Env(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_ORCHESTRATE_PLAN_REQUIRES_APPROVAL", "true")
	cmd := newOrchestrateCmd()
	got, err := orchestratePlanRequiresApprovalOverride(cmd)
	if err != nil {
		t.Fatalf("orchestratePlanRequiresApprovalOverride: %v", err)
	}
	if got == nil || !*got {
		t.Fatalf("override = %v, want true", got)
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
	cmd.SetContext(t.Context())
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

// setRowWorkTool stamps WorkTool on the seeded row so the verifier-only
// path can resolve the worker for its fix loop without a prior worker
// phase running.
func setRowWorkTool(t *testing.T, id, tool string) {
	t.Helper()
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	row.WorkTool = tool
	if err := s.PutTask(row); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
}

// stagePlan writes plan.md into the seeded task dir so the worker
// shell-out finds the stored plan when planning is skipped.
func stagePlan(t *testing.T, id string) {
	t.Helper()
	tasksDir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tasksDir, id, tasks.PlanFileName), []byte("1. step"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunOrchestrate_FromWorkIgnoresProjectApproval pins the fix for
// the re-work / re-verify regression: when Phase=FromWork and
// PlanRequiresApproval is nil, the project-level
// plan_requires_approval default must NOT be consulted (otherwise
// projects opted into approval would hit the conflict guard and the
// post-planner re-runs would refuse to proceed).
func TestRunOrchestrate_FromWorkIgnoresProjectApproval(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	putProjectPlanRequiresApproval(t, "true")
	id := seedOrchestrateTask(t, "scripted")
	stagePlan(t, id)
	stub := newChainAgent("scripted")
	stub.verdicts = []string{"VERDICT: PASS"}

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID: id,
		Phase:  orchestrator.RunPhaseFromWork,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	if stub.planCalls.Load() != 0 {
		t.Fatalf("plan calls = %d, want 0", stub.planCalls.Load())
	}
	if stub.workCalls.Load() != 1 || stub.verifyCalls.Load() != 1 {
		t.Fatalf("call counts: work=%d verify=%d, want 1/1",
			stub.workCalls.Load(), stub.verifyCalls.Load())
	}
}

// TestRunOrchestrate_VerifyOnlyIgnoresProjectApproval pins the
// verifier-only re-run (j tasks re-verify / resume-verify): with
// project.plan_requires_approval=true and PlanRequiresApproval nil,
// Phase=VerifyOnly must reach the verifier without erroring.
func TestRunOrchestrate_VerifyOnlyIgnoresProjectApproval(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	putProjectPlanRequiresApproval(t, "true")
	id := seedOrchestrateTask(t, "scripted")
	stagePlan(t, id)
	setRowWorkTool(t, id, "scripted")
	stub := newChainAgent("scripted")
	stub.verdicts = []string{"VERDICT: PASS"}

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID: id,
		Phase:  orchestrator.RunPhaseVerifyOnly,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	if stub.planCalls.Load() != 0 || stub.workCalls.Load() != 0 {
		t.Fatalf("plan/work should not run on verify-only: plan=%d work=%d",
			stub.planCalls.Load(), stub.workCalls.Load())
	}
	if stub.verifyCalls.Load() != 1 {
		t.Fatalf("verify calls = %d, want 1", stub.verifyCalls.Load())
	}
}

// TestRunOrchestrate_FromWorkConflictExplicitOverrideOnly pins that
// the conflict guard only fires on an *explicit* approval=true
// override, not on the project default. The sub-case with the project
// default ALSO set to true keeps the explicit-override path
// load-bearing.
func TestRunOrchestrate_FromWorkConflictExplicitOverrideOnly(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	putProjectPlanRequiresApproval(t, "true")
	id := seedOrchestrateTask(t, "scripted")
	stub := newChainAgent("scripted")

	err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: requirePlanApproval(),
		Phase:                orchestrator.RunPhaseFromWork,
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	})
	if err == nil || !strings.Contains(err.Error(), "from-work") {
		t.Fatalf("err = %v, want from-work incompatibility", err)
	}
	if stub.planCalls.Load()+stub.workCalls.Load()+stub.verifyCalls.Load() != 0 {
		t.Fatalf("no agent should run when conflict guard fires")
	}
}

// TestRunOrchestrate_InfersPlanResumeFromRow pins the resume-plan
// inference contract end to end: a row with a non-empty
// PlanResumeSession causes the orchestrator's planner phase to (a)
// skip the agent's NewResumeID, (b) deliver the row's stored session
// to PlanRequest.ResumeChatID, (c) set PlanRequest.Resume=true so
// the backend selects the resume prompt, and (d) preserve the row's
// PlanResumeSession after the run. Mirrors how the worker /
// verifier infer resume mode from their own *ResumeSession fields.
func TestRunOrchestrate_InfersPlanResumeFromRow(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedOrchestrateTask(t, "scripted")
	seedRowPlanPendingApprovalWithSession(t, id, "prior-cursor")
	stagePlan(t, id)
	stub := newChainAgent("scripted")
	stub.newResumeIDPanics = true
	stub.verdicts = []string{"VERDICT: PASS"}

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: requirePlanApproval(),
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	if stub.newResumeIDCalls.Load() != 0 {
		t.Fatalf("NewResumeID calls = %d, want 0 on resume run",
			stub.newResumeIDCalls.Load())
	}
	if stub.lastPlanReq.ResumeChatID != "prior-cursor" {
		t.Fatalf("PlanRequest.ResumeChatID = %q, want prior-cursor",
			stub.lastPlanReq.ResumeChatID)
	}
	if !stub.lastPlanReq.Resume {
		t.Fatal("PlanRequest.Resume = false, want true")
	}
	row := readOrchestrateTaskRow(t, id)
	if row.PlanResumeSession != "prior-cursor" {
		t.Fatalf("PlanResumeSession = %q, want prior-cursor (preserved)",
			row.PlanResumeSession)
	}
}

// seedRowPlanPendingApprovalWithSession flips the seeded row to
// plan-pending-approval and stamps PlanResumeSession so the
// resume-plan branch has a session to reuse.
func seedRowPlanPendingApprovalWithSession(t *testing.T, id, session string) {
	t.Helper()
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	row.Status = tasks.StatusPlanPendingApproval
	row.PlanResumeSession = session
	if err := s.PutTask(row); err != nil {
		t.Fatalf("PutTask: %v", err)
	}
}

// TestRunOrchestrate_FromWorkExplicitFalseAllowed pins that an
// explicit PlanRequiresApproval=false short-circuits the project
// default the same way nil does — worker/verifier both run.
func TestRunOrchestrate_FromWorkExplicitFalseAllowed(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	putProjectPlanRequiresApproval(t, "true")
	id := seedOrchestrateTask(t, "scripted")
	stagePlan(t, id)
	stub := newChainAgent("scripted")
	stub.verdicts = []string{"VERDICT: PASS"}

	if err := RunOrchestrate(t.Context(), OrchestrateOptions{
		TaskID:               id,
		PlanRequiresApproval: noPlanApproval(),
		Phase:                orchestrator.RunPhaseFromWork,
		Stdin:                strings.NewReader(""),
		Stdout:               io.Discard,
		Stderr:               io.Discard,
		Agents:               []codingagents.Agent{stub},
	}); err != nil {
		t.Fatalf("RunOrchestrate: %v", err)
	}
	if stub.planCalls.Load() != 0 {
		t.Fatalf("plan calls = %d, want 0", stub.planCalls.Load())
	}
	if stub.workCalls.Load() != 1 || stub.verifyCalls.Load() != 1 {
		t.Fatalf("call counts: work=%d verify=%d, want 1/1",
			stub.workCalls.Load(), stub.verifyCalls.Load())
	}
}
