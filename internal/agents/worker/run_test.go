package worker

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
	"github.com/spacelions/j/internal/resolver"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// runTestAgent implements codingagents.Agent for Execute tests.
type runTestAgent struct {
	name   string
	models []string

	workCalls   int
	workErr     error
	workPid     int
	lastWorkReq codingagents.WorkRequest
	resumeID    string
	resumeIDErr error
}

func newRunTestAgent(name string) *runTestAgent {
	return &runTestAgent{name: name, models: []string{"m1"}}
}

func (a *runTestAgent) Name() string                                 { return a.name }
func (a *runTestAgent) ListModels(context.Context) ([]string, error) { return a.models, nil }
func (a *runTestAgent) CheckLogin(context.Context) error             { return nil }
func (a *runTestAgent) Plan(context.Context, codingagents.PlanRequest) (int, error) {
	return 0, errors.New("should not be called")
}

func (a *runTestAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("should not be called")
}

func (*runTestAgent) FormatLog(line []byte) []byte { return line }

func (a *runTestAgent) NewResumeID(context.Context) (string, error) {
	if a.resumeIDErr != nil {
		return "", a.resumeIDErr
	}
	if a.resumeID != "" {
		return a.resumeID, nil
	}
	return "00000000-0000-4000-8000-000000000001", nil
}

func (a *runTestAgent) Work(_ context.Context, req codingagents.WorkRequest) (int, error) {
	a.workCalls++
	a.lastWorkReq = req
	return a.workPid, a.workErr
}

// fakeRunUI is a scripted UI fake for Execute tests.
type fakeRunUI struct {
	pickTaskReturn string
	pickTaskOK     bool
	pickTaskErr    error

	selectTool  string
	selectModel string
	selectErr   error

	confirmReturn bool
	confirmErr    error
}

func (u *fakeRunUI) PickTask(context.Context, string, []tasks.Task) (string, bool, error) {
	return u.pickTaskReturn, u.pickTaskOK, u.pickTaskErr
}

func (u *fakeRunUI) SelectTool(context.Context, []string) (string, error) {
	return u.selectTool, u.selectErr
}

func (u *fakeRunUI) SelectModel(context.Context, []string) (string, error) {
	return u.selectModel, u.selectErr
}

func (u *fakeRunUI) ConfirmStatusOverride(context.Context, string, string, string) (bool, error) {
	return u.confirmReturn, u.confirmErr
}

func setupRunEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	testutil.Init(t)
	testutil.SeedAgentBucket(t, store.BucketWorker, "cursor", "m1")
	testutil.SeedAgentBucket(t, store.BucketPlanner, "cursor", "m1")
	testutil.SeedAgentBucket(t, store.BucketVerifier, "cursor", "m1")
}

func seedPlanDoneTask(t *testing.T) string {
	t.Helper()
	id := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, tasks.PlanFileName), []byte("1. step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.SeedTaskRow(t, tasks.Task{
		ID:       id,
		Status:   tasks.StatusPlanDone,
		PlanTool: "cursor",
		Summary:  "test task",
	})
	return id
}

func TestRun_NoAgentsError(t *testing.T) {
	ctx := t.Context()
	err := Execute(ctx, ExecuteOptions{Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v, want 'no coding agents' error", err)
	}
}

func TestRun_HappyPath(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	var stdout bytes.Buffer
	err := Execute(t.Context(), ExecuteOptions{
		TaskID: id,
		Yes:    true,
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{},
		Tool:   "cursor",
		Model:  "m1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
	row := testutil.ReadTaskRow(t, id)
	if row.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", row.Status)
	}
	if !strings.Contains(stdout.String(), "working on task "+id) {
		t.Fatalf("stdout = %q, missing coding message", stdout.String())
	}
}

func TestRun_WorkErrorPromotesToHelp(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.workErr = errors.New("worker boom")
	err := Execute(t.Context(), ExecuteOptions{
		TaskID: id,
		Yes:    true,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{},
		Tool:   "cursor",
		Model:  "m1",
	})
	if err == nil || !strings.Contains(err.Error(), "worker boom") {
		t.Fatalf("err = %v, want 'worker boom'", err)
	}
	row := testutil.ReadTaskRow(t, id)
	if row.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", row.Status)
	}
}

func TestRun_ConfirmStatusOverrideDeclined(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	row := testutil.ReadTaskRow(t, id)
	row.Status = tasks.StatusCompleted
	testutil.SeedTaskRow(t, row)
	agent := newRunTestAgent("cursor")
	err := Execute(t.Context(), ExecuteOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{confirmReturn: false},
		Tool:   "cursor",
		Model:  "m1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if agent.workCalls != 0 {
		t.Fatalf("workCalls = %d, want 0 (confirm declined)", agent.workCalls)
	}
}

func TestRun_RecordsAgentLog(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.workPid = 42
	var stdout bytes.Buffer
	err := Execute(t.Context(), ExecuteOptions{
		TaskID: id,
		Yes:    true,
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{},
		Tool:   "cursor",
		Model:  "m1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	row := testutil.ReadTaskRow(t, id)
	if row.AgentLogPath == "" {
		t.Fatalf("AgentLogPath = %q, want non-empty", row.AgentLogPath)
	}
	if !strings.Contains(stdout.String(), "running in background") {
		t.Fatalf("stdout = %q, missing background message", stdout.String())
	}
}

func TestRun_WaitForCompletion_Success(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.workPid = 0
	err := Execute(t.Context(), ExecuteOptions{
		TaskID:            id,
		Yes:               true,
		Stdin:             strings.NewReader(""),
		Stdout:            io.Discard,
		Stderr:            io.Discard,
		Agents:            []codingagents.Agent{agent},
		UI:                &fakeRunUI{},
		Tool:              "cursor",
		Model:             "m1",
		WaitForCompletion: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
}

func TestRun_AppliesDefaults(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	opts := ExecuteOptions{
		TaskID: id,
		Agents: []codingagents.Agent{agent},
		Yes:    true,
		Tool:   "cursor",
		Model:  "m1",
	}
	opts = opts.withDefaults()
	if opts.Stdin == nil || opts.Stdout == nil || opts.Stderr == nil {
		t.Fatal("withDefaults should fill nil streams")
	}
	if opts.UI == nil {
		t.Fatal("withDefaults should give default UI")
	}
}

func TestRun_ExplicitToolModelSkipsPersistence(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	err := Execute(t.Context(), ExecuteOptions{
		TaskID: id,
		Yes:    true,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{},
		Tool:   "cursor",
		Model:  "m1",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
}

func TestRun_NoPlanTasks(t *testing.T) {
	setupRunEnv(t)
	agent := newRunTestAgent("cursor")
	err := Execute(t.Context(), ExecuteOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{},
		Tool:   "cursor",
		Model:  "m1",
	})
	if err == nil || !strings.Contains(err.Error(), "no tasks") {
		t.Fatalf("err = %v, want 'no tasks to work' error", err)
	}
	if agent.workCalls != 0 {
		t.Fatalf("workCalls = %d, want 0", agent.workCalls)
	}
}

func TestRun_NewResumeIDError(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.resumeIDErr = errors.New("resume id failure")
	err := Execute(t.Context(), ExecuteOptions{
		TaskID: id,
		Yes:    true,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{},
		Tool:   "cursor",
		Model:  "m1",
	})
	if err != nil {
		t.Fatalf("Execute: %v (NewResumeID error should not abort)", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
}

func TestRun_ConfirmStatusOverrideError(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	row := testutil.ReadTaskRow(t, id)
	row.Status = tasks.StatusCompleted
	testutil.SeedTaskRow(t, row)
	agent := newRunTestAgent("cursor")
	err := Execute(t.Context(), ExecuteOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{confirmErr: errors.New("confirm fail")},
		Tool:   "cursor",
		Model:  "m1",
	})
	if err == nil || !strings.Contains(err.Error(), "confirm fail") {
		t.Fatalf("err = %v, want 'confirm fail'", err)
	}
	if agent.workCalls != 0 {
		t.Fatalf("workCalls = %d, want 0", agent.workCalls)
	}
}

// TestRun_ResumeFromClarificationFlag pins that Execute sets
// WorkRequest.ResumeFromClarification to true ONLY when the row is
// in resume mode AND a clarification.md exists in the per-task dir.
// The worker deletes the file at the end of the resume turn so the
// next Finish() routes to the natural terminal status.
func TestRun_ResumeFromClarificationFlag(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(taskDir, tasks.ClarificationFileName),
		[]byte("what?"), 0o600,
	); err != nil {
		t.Fatalf("write clarification: %v", err)
	}
	row := testutil.ReadTaskRow(t, id)
	row.Status = tasks.StatusWorking
	row.WorkResumeSession = "prior-cursor"
	row.WorkTool = "cursor"
	row.WorkModel = "m1"
	testutil.SeedTaskRow(t, row)

	agent := newRunTestAgent("cursor")
	if err := Execute(t.Context(), ExecuteOptions{
		TaskID: id,
		Yes:    true,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{},
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !agent.lastWorkReq.Resume {
		t.Fatal("Resume = false, want true on resume run")
	}
	if !agent.lastWorkReq.ResumeFromClarification {
		t.Fatal(
			"ResumeFromClarification = false, " +
				"want true with clarification.md present",
		)
	}
}

// TestRun_ResumeWithoutClarificationFile pins the no-file branch:
// resume run without a clarification.md leaves
// ResumeFromClarification=false (regular resume template).
func TestRun_ResumeWithoutClarificationFile(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	row := testutil.ReadTaskRow(t, id)
	row.Status = tasks.StatusWorking
	row.WorkResumeSession = "prior-cursor"
	row.WorkTool = "cursor"
	row.WorkModel = "m1"
	testutil.SeedTaskRow(t, row)

	agent := newRunTestAgent("cursor")
	if err := Execute(t.Context(), ExecuteOptions{
		TaskID: id,
		Yes:    true,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{},
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if agent.lastWorkReq.ResumeFromClarification {
		t.Fatal(
			"ResumeFromClarification = true, " +
				"want false without clarification.md",
		)
	}
}

func TestRun_WaitForCompletion_PIDZero(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.workPid = 0
	err := Execute(t.Context(), ExecuteOptions{
		TaskID:            id,
		Yes:               true,
		Stdin:             strings.NewReader(""),
		Stdout:            io.Discard,
		Stderr:            io.Discard,
		Agents:            []codingagents.Agent{agent},
		UI:                &fakeRunUI{},
		Tool:              "cursor",
		Model:             "m1",
		WaitForCompletion: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
}

func TestResolveWorker_ResumeUnknownTool(t *testing.T) {
	agent := newRunTestAgent("cursor")
	_, _, _, err := resolveWorker(
		t.Context(),
		ExecuteOptions{Agents: []codingagents.Agent{agent}},
		resolverWorkPlan("ghost", "m1", "session-1"),
		true,
	)
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("resolveWorker err = %v, want unknown tool", err)
	}
}

func TestResolveWorker_SelectWorkerError(t *testing.T) {
	agent := newRunTestAgent("cursor")
	_, _, _, err := resolveWorker(
		t.Context(),
		ExecuteOptions{
			Agents: []codingagents.Agent{agent},
			Tool:   "ghost",
			Stderr: io.Discard,
			UI:     &fakeRunUI{},
		},
		resolverWorkPlan("", "", ""),
		false,
	)
	if err == nil {
		t.Fatal("resolveWorker err = nil, want select-worker error")
	}
}

func TestLookupResumeAgent(t *testing.T) {
	agent := newRunTestAgent("cursor")
	if got, ok := lookupResumeAgent(
		[]codingagents.Agent{agent}, "cursor",
	); !ok || got != agent {
		t.Fatalf("lookupResumeAgent found = (%v, %v), want agent,true", got, ok)
	}
	if got, ok := lookupResumeAgent(
		[]codingagents.Agent{agent}, "",
	); ok || got != nil {
		t.Fatalf("lookupResumeAgent empty = (%v, %v), want nil,false", got, ok)
	}
	if got, ok := lookupResumeAgent(
		[]codingagents.Agent{agent}, "ghost",
	); ok || got != nil {
		t.Fatalf("lookupResumeAgent miss = (%v, %v), want nil,false", got, ok)
	}
}

func resolverWorkPlan(tool, model, session string) resolver.WorkPlan {
	return resolver.WorkPlan{
		Task: tasks.Task{
			ID:                tasks.NewTaskID(),
			Status:            tasks.StatusWorking,
			WorkTool:          tool,
			WorkModel:         model,
			WorkResumeSession: session,
		},
	}
}
