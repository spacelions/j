package planner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
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

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "# task\nbody"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{ID: taskID, Status: tasks.StatusPlanning, Summary: "task"})
	seedPlanApproval(t, false)

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
	if got.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done", got.Status)
	}
}

func TestNew_ShellOutPlanFails(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "x"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{ID: taskID, Status: tasks.StatusPlanning, Summary: "task"})

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
	if got.Status != tasks.StatusHelp {
		t.Fatalf("Status = %q, want help", got.Status)
	}
}

func TestNew_ShellOutResolverError(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	testutil.SeedAgentBucket(t, store.BucketPlanner, "ghost", "m1")
	stub := newScriptedPlanAgent("scripted")
	a, err := New(Config{
		TaskID: "task",
		Agents: []codingagents.Agent{stub},
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = testutil.DrainAgentForError(t, a)
	if err == nil || !strings.Contains(err.Error(), `unknown tool "ghost"`) {
		t.Fatalf("err = %v, want unknown-tool resolver error", err)
	}
	if stub.planCalls != 0 {
		t.Fatalf("Plan calls = %d, want 0", stub.planCalls)
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
	if err := testutil.WriteFile(taskDir+"/requirements.md", "y"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{ID: taskID, Status: tasks.StatusPlanning, Summary: "task"})

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

// TestExecute_ResumeFromStoredSession pins the resume contract:
// when Execute runs on a row carrying a non-empty PlanResumeSession,
// the agent's NewResumeID MUST NOT be called, the PlanRequest MUST
// carry Resume=true and ResumeChatID equal to the row's stored
// value, and the row's PlanResumeSession value MUST remain
// unchanged after the run. Mirrors the worker / verifier inference
// pattern (resume vs fresh is read from the row, not a flag).
func TestExecute_ResumeFromStoredSession(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "# x\nbody"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                taskID,
		Status:            tasks.StatusPlanPendingApproval,
		PlanTool:          "scripted",
		PlanModel:         "m1",
		PlanResumeSession: "prior-cursor",
		Summary:           "task",
	})
	seedPlanApproval(t, false)

	stub := newScriptedPlanAgent("scripted")
	stub.panicOnNewResumeID = true
	if err := Execute(t.Context(), Options{
		TaskID: taskID,
		Agent:  stub,
		Model:  "m1",
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stub.planCalls != 1 {
		t.Fatalf("Plan calls = %d, want 1", stub.planCalls)
	}
	if stub.lastReq.ResumeChatID != "prior-cursor" {
		t.Fatalf("ResumeChatID = %q, want prior-cursor", stub.lastReq.ResumeChatID)
	}
	if !stub.lastReq.Resume {
		t.Fatal("PlanRequest.Resume = false, want true on resume run")
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.PlanResumeSession != "prior-cursor" {
		t.Fatalf("PlanResumeSession = %q, want prior-cursor (must not be overwritten)",
			got.PlanResumeSession)
	}
}

// TestExecute_ResumeFromClarificationFlag pins that Execute sets
// PlanRequest.ResumeFromClarification to true ONLY when the row is
// in resume mode AND a clarification.md exists in the per-task dir.
// The agent deletes that file at the end of its turn so the
// follow-up resume run (without the file) flips the flag back to
// false — Finish() then routes to the natural terminal status. This
// test exercises the with-file branch.
func TestExecute_ResumeFromClarificationFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "# x\nbody"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := testutil.WriteFile(
		taskDir+"/"+tasks.ClarificationFileName, "what?"); err != nil {
		t.Fatalf("write clarification: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                taskID,
		Status:            tasks.StatusPlanning,
		PlanTool:          "scripted",
		PlanModel:         "m1",
		PlanResumeSession: "prior-cursor",
		Summary:           "task",
	})
	seedPlanApproval(t, false)

	stub := newScriptedPlanAgent("scripted")
	if err := Execute(t.Context(), Options{
		TaskID: taskID,
		Agent:  stub,
		Model:  "m1",
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !stub.lastReq.ResumeFromClarification {
		t.Fatal(
			"PlanRequest.ResumeFromClarification = false, " +
				"want true with clarification.md present",
		)
	}
	if !stub.lastReq.Resume {
		t.Fatal("PlanRequest.Resume should still be true")
	}
}

// TestExecute_ResumeWithoutClarificationFile pins the no-file
// branch: a resume run whose per-task dir has no clarification.md
// must leave ResumeFromClarification=false so the agent uses the
// regular resume template.
func TestExecute_ResumeWithoutClarificationFile(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "x"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:                taskID,
		Status:            tasks.StatusPlanning,
		PlanTool:          "scripted",
		PlanModel:         "m1",
		PlanResumeSession: "prior-cursor",
		Summary:           "task",
	})
	seedPlanApproval(t, false)

	stub := newScriptedPlanAgent("scripted")
	if err := Execute(t.Context(), Options{
		TaskID: taskID,
		Agent:  stub,
		Model:  "m1",
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stub.lastReq.ResumeFromClarification {
		t.Fatal(
			"PlanRequest.ResumeFromClarification = true, " +
				"want false without clarification.md",
		)
	}
}

// TestExecute_FreshFromEmptySession pins the restart contract: an
// empty PlanResumeSession means "fresh" — Execute mints a new id via
// NewResumeID, sets PlanRequest.Resume=false, and stamps the new id
// into the row.
func TestExecute_FreshFromEmptySession(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "# x\nbody"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedAgentBucket(t, store.BucketPlanner, "scripted", "m1")
	testutil.SeedTaskRow(t, tasks.Task{
		ID:        taskID,
		Status:    tasks.StatusPlanDone,
		PlanTool:  "scripted",
		PlanModel: "m1",
		Summary:   "task",
	})
	seedPlanApproval(t, false)

	stub := newScriptedPlanAgent("scripted")
	if err := Execute(t.Context(), Options{
		TaskID: taskID,
		Agent:  stub,
		Model:  "m1",
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stub.lastReq.ResumeChatID != "rid" {
		t.Fatalf("ResumeChatID = %q, want freshly-minted rid",
			stub.lastReq.ResumeChatID)
	}
	if stub.lastReq.Resume {
		t.Fatal("PlanRequest.Resume = true, want false on fresh run")
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.PlanResumeSession != "rid" {
		t.Fatalf("PlanResumeSession = %q, want rid (newly minted)",
			got.PlanResumeSession)
	}
}

func TestExecute_NoWaitSkipsPostRunCapture(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "x"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedTaskRow(t, tasks.Task{
		ID:      taskID,
		Status:  tasks.StatusPlanning,
		Summary: "task",
	})

	stub := newScriptedPlanAgent("scripted")
	stub.mintedID = ""
	stub.captureID = "captured-after-run"
	if err := Execute(t.Context(), Options{
		TaskID:            taskID,
		Agent:             stub,
		Model:             "m1",
		WaitForCompletion: false,
		Stderr:            io.Discard,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stub.captureCalls != 0 {
		t.Fatalf("CaptureResumeID calls = %d, want 0", stub.captureCalls)
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.PlanResumeSession != "" {
		t.Fatalf("PlanResumeSession = %q, want empty", got.PlanResumeSession)
	}
}

func TestExecute_NoWaitCapturesActiveResumeSession(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(
		taskDir+"/requirements.md", "x",
	); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedTaskRow(t, tasks.Task{
		ID:      taskID,
		Status:  tasks.StatusPlanning,
		Summary: "task",
	})

	stub := newScriptedPlanAgent("scripted")
	stub.mintedID = ""
	stub.captureID = "captured-active-plan"
	stub.planPID = os.Getpid()
	if err := Execute(t.Context(), Options{
		TaskID:            taskID,
		Agent:             stub,
		Model:             "m1",
		WaitForCompletion: false,
		Stderr:            io.Discard,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stub.captureCalls == 0 {
		t.Fatal("CaptureResumeID calls = 0, want active capture")
	}
	got := testutil.ReadTaskRow(t, taskID)
	if got.PlanResumeSession != "captured-active-plan" {
		t.Fatalf("PlanResumeSession = %q, want captured-active-plan",
			got.PlanResumeSession)
	}
}

func TestExecute_WaitForCompletionError(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(
		taskDir+"/requirements.md", "x",
	); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedTaskRow(t, tasks.Task{
		ID:      taskID,
		Status:  tasks.StatusPlanning,
		Summary: "task",
	})

	stub := newScriptedPlanAgent("scripted")
	stub.planPID = os.Getpid()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = Execute(ctx, Options{
		TaskID:            taskID,
		Agent:             stub,
		Model:             "m1",
		WaitForCompletion: true,
		Stderr:            io.Discard,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestExecute_NewResumeIDError covers the DangerousDialogBox warning
// path in beginPlanSession when NewResumeID returns an error.
// The plan run continues with an empty resume ID.
func TestExecute_NewResumeIDError(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(taskDir+"/requirements.md", "x"); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedTaskRow(t, tasks.Task{
		ID:      taskID,
		Status:  tasks.StatusPlanning,
		Summary: "task",
	})
	stub := newScriptedPlanAgent("scripted")
	stub.newResumeIDErr = errors.New("NewResumeID boom")
	var stderr bytes.Buffer
	if err := Execute(t.Context(), Options{
		TaskID: taskID,
		Agent:  stub,
		Model:  "m1",
		Stderr: &stderr,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stderr.String(), "NewResumeID boom") {
		t.Fatalf("stderr = %q, want NewResumeID error warning", stderr.String())
	}
}

func TestExecute_NilStderrAndMustReadWarning(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)

	taskID := tasks.NewTaskID()
	taskDir, err := tasks.EnsureDir(taskID)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if err := testutil.WriteFile(
		taskDir+"/requirements.md", "x",
	); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	testutil.SeedTaskRow(t, tasks.Task{
		ID:      taskID,
		Status:  tasks.StatusPlanning,
		Summary: "task",
	})
	settingsPath := store.DefaultPath()
	if err := os.Remove(settingsPath); err != nil {
		t.Fatalf("Remove settings: %v", err)
	}
	if err := os.Mkdir(settingsPath, 0o755); err != nil {
		t.Fatalf("Mkdir settings: %v", err)
	}

	stub := newScriptedPlanAgent("scripted")
	if err := Execute(t.Context(), Options{
		TaskID: taskID,
		Agent:  stub,
		Model:  "m1",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// TestExecute_ResolvePlanTaskError covers the ResolvePlanTask error branch in
// Execute: an unknown task ID causes ResolvePlanTask to fail.
func TestExecute_ResolvePlanTaskError(t *testing.T) {
	t.Chdir(t.TempDir())
	testutil.Init(t)
	stub := newScriptedPlanAgent("scripted")
	err := Execute(t.Context(), Options{
		TaskID: "ghost-id",
		Agent:  stub,
		Model:  "m1",
		Stderr: io.Discard,
	})
	if err == nil {
		t.Fatal("Execute with unknown task id: expected error, got nil")
	}
}

func TestReadPlanArtifacts_MissingFilesUseNormalMessages(t *testing.T) {
	tests := []struct {
		name        string
		requirement string
		plan        string
		missing     []string
	}{
		{
			name:    "missing requirements",
			plan:    "plan-ok",
			missing: []string{"requirements.md"},
		},
		{
			name:        "missing plan",
			requirement: "requirements-ok",
			missing:     []string{"plan.md"},
		},
		{
			name:    "missing both",
			missing: []string{"requirements.md", "plan.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			requirementsPath := filepath.Join(dir, "requirements.md")
			planPath := filepath.Join(dir, "plan.md")
			if tt.requirement != "" {
				if err := testutil.WriteFile(
					requirementsPath, tt.requirement,
				); err != nil {
					t.Fatalf("write requirements: %v", err)
				}
			}
			if tt.plan != "" {
				if err := testutil.WriteFile(planPath, tt.plan); err != nil {
					t.Fatalf("write plan: %v", err)
				}
			}

			var stderr bytes.Buffer
			gotReq, gotPlan := readPlanArtifacts(
				&stderr, nil, requirementsPath, planPath,
			)
			if gotReq != tt.requirement {
				t.Fatalf("refinedReq = %q, want %q", gotReq, tt.requirement)
			}
			if gotPlan != tt.plan {
				t.Fatalf("planMD = %q, want %q", gotPlan, tt.plan)
			}

			stripped := ansi.Strip(stderr.String())
			assertNoDialogBorder(t, stripped)
			for _, filename := range tt.missing {
				path := filepath.Join(dir, filename)
				want := "J: missing planner artifact " + path + "\n"
				if !strings.Contains(stripped, want) {
					t.Fatalf("stderr = %q, want message %q", stripped, want)
				}
			}
		})
	}
}

func TestReadPlanArtifacts_NonMissingReadErrorUsesDangerousText(t *testing.T) {
	dir := t.TempDir()
	requirementsPath := filepath.Join(dir, "requirements.md")
	planPath := filepath.Join(dir, "plan.md")
	if err := os.Mkdir(requirementsPath, 0o755); err != nil {
		t.Fatalf("mkdir requirements path: %v", err)
	}
	if err := testutil.WriteFile(planPath, "plan-ok"); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	var stderr bytes.Buffer
	gotReq, gotPlan := readPlanArtifacts(
		&stderr, nil, requirementsPath, planPath,
	)
	if gotReq != "" {
		t.Fatalf("refinedReq = %q, want empty", gotReq)
	}
	if gotPlan != "plan-ok" {
		t.Fatalf("planMD = %q, want plan-ok", gotPlan)
	}

	stripped := ansi.Strip(stderr.String())
	assertNoDialogBorder(t, stripped)
	want := "J: read " + requirementsPath + ": "
	if !strings.Contains(stripped, want) {
		t.Fatalf("stderr = %q, want read warning prefix %q", stripped, want)
	}
	if strings.Contains(stripped, "missing planner artifact") {
		t.Fatalf("stderr = %q, want non-missing read warning", stripped)
	}
}

// scriptedPlanAgent stands in for a real codingagents.Agent. Plan
// writes the per-task requirements.md / plan.md inline so plan.Run's
// finishPlan promotes the row to plan-done synchronously.
type scriptedPlanAgent struct {
	name               string
	models             []string
	mintedID           string
	captureID          string
	captureCalls       int
	planCalls          int
	planPID            int
	planErr            error
	lastReq            codingagents.PlanRequest
	panicOnNewResumeID bool
	newResumeIDErr     error
}

func newScriptedPlanAgent(name string) *scriptedPlanAgent {
	return &scriptedPlanAgent{
		name:     name,
		models:   []string{"m1"},
		mintedID: "rid",
	}
}

func (a *scriptedPlanAgent) Name() string                                 { return a.name }
func (a *scriptedPlanAgent) ListModels(context.Context) ([]string, error) { return a.models, nil }
func (a *scriptedPlanAgent) CheckLogin(context.Context) error             { return nil }
func (a *scriptedPlanAgent) NewResumeID(context.Context) (string, error) {
	if a.panicOnNewResumeID {
		panic("NewResumeID must not be called on resume runs")
	}
	if a.newResumeIDErr != nil {
		return "", a.newResumeIDErr
	}
	return a.mintedID, nil
}

func (a *scriptedPlanAgent) CaptureResumeID(
	context.Context, string, time.Time,
) (string, error) {
	a.captureCalls++
	return a.captureID, nil
}

func (a *scriptedPlanAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.planCalls++
	a.lastReq = req
	if a.planErr != nil {
		return 0, a.planErr
	}
	if err := testutil.WriteFile(req.RequirementsOutputPath, "plan-refined-requirements"); err != nil {
		return 0, err
	}
	if err := testutil.WriteFile(req.PlanOutputPath, "1. step"); err != nil {
		return 0, err
	}
	return a.planPID, nil
}

func (a *scriptedPlanAgent) Work(context.Context, codingagents.WorkRequest) (int, error) {
	return 0, errors.New("scriptedPlanAgent.Work should not be called")
}

func (a *scriptedPlanAgent) Verify(context.Context, codingagents.VerifyRequest) (int, error) {
	return 0, errors.New("scriptedPlanAgent.Verify should not be called")
}

func (*scriptedPlanAgent) FormatLog(line []byte) []byte { return line }

func seedPlanApproval(t *testing.T, v bool) {
	t.Helper()
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	val := "false"
	if v {
		val = "true"
	}
	if err := s.Put(store.BucketProject,
		store.KeyPlanRequiresApproval, val); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func assertNoDialogBorder(t *testing.T, s string) {
	t.Helper()
	for _, glyph := range []string{"┌", "┐", "└", "┘"} {
		if strings.Contains(s, glyph) {
			t.Fatalf("stderr contains dialog border glyph %q: %q", glyph, s)
		}
	}
}
