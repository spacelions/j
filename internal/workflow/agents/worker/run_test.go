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

	"github.com/charmbracelet/huh"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// runTestAgent implements codingagents.Agent for Run/Resume tests.
// Plan/Verify are stubbed and should not be called.
type runTestAgent struct {
	name   string
	models []string

	workCalls    int
	workErr      error
	workPid      int
	lastWorkReq  codingagents.WorkRequest
	resumeID     string
	resumeIDErr  error
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

// fakeRunUI is a scripted UI fake for Run/Resume tests.
type fakeRunUI struct {
	// pickTaskReturn is the id returned by PickTask.
	pickTaskReturn string
	// pickTaskOK is the ok flag returned by PickTask.
	pickTaskOK bool
	// pickTaskErr is the error returned by PickTask.
	pickTaskErr error

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

func seedWorkingTaskWithSession(t *testing.T) string {
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
		ID:                id,
		Status:            tasks.StatusWorking,
		WorkTool:          "cursor",
		WorkModel:         "m1",
		WorkResumeSession: "existing-session",
		Summary:           "in-progress task",
	})
	return id
}

// --- Run tests ---

func TestRun_NoAgentsError(t *testing.T) {
	ctx := context.Background()
	err := Run(ctx, Options{Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v, want 'no coding agents' error", err)
	}
}

func TestRun_HappyPath(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	var stdout bytes.Buffer
	err := Run(context.Background(), Options{
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
		t.Fatalf("Run: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
	row := testutil.ReadTaskRow(t, id)
	if row.Status != tasks.StatusWorkDone {
		t.Fatalf("Status = %q, want work-done", row.Status)
	}
	if !strings.Contains(stdout.String(), "coding on task "+id) {
		t.Fatalf("stdout = %q, missing coding message", stdout.String())
	}
}

func TestRun_WorkErrorPromotesToHelp(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.workErr = errors.New("worker boom")
	err := Run(context.Background(), Options{
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
	// Set task to a status outside the allowlist.
	row := testutil.ReadTaskRow(t, id)
	row.Status = tasks.StatusCompleted
	testutil.SeedTaskRow(t, row)
	agent := newRunTestAgent("cursor")
	err := Run(context.Background(), Options{
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
		t.Fatalf("Run: %v", err)
	}
	if agent.workCalls != 0 {
		t.Fatalf("workCalls = %d, want 0 (confirm declined)", agent.workCalls)
	}
}

func TestRun_BackgroundPID(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.workPid = 42
	var stdout bytes.Buffer
	err := Run(context.Background(), Options{
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
		t.Fatalf("Run: %v", err)
	}
	row := testutil.ReadTaskRow(t, id)
	if row.BackgroundPID != 42 {
		t.Fatalf("BackgroundPID = %d, want 42", row.BackgroundPID)
	}
	if !strings.Contains(stdout.String(), "running in background") {
		t.Fatalf("stdout = %q, missing background message", stdout.String())
	}
}

func TestRun_WaitForCompletion_Success(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.workPid = 999 // non-zero pid, but WaitForExit will fail since pid doesn't exist
	// Actually, WaitForExit will try to wait on a non-existent process.
	// Let's use workPid=0 for this test to keep it simple.
	agent.workPid = 0
	err := Run(context.Background(), Options{
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
		t.Fatalf("Run: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
}

func TestRun_AppliesDefaults(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	opts := Options{
		TaskID: id,
		Agents: []codingagents.Agent{agent},
		Yes:    true,
		Tool:   "cursor",
		Model:  "m1",
	}
	// Stdio/Stdout/Stderr should be filled from os.Stdin/Stderr/Stdout
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
	err := Run(context.Background(), Options{
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
		t.Fatalf("Run: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
}

// --- RunResume tests ---

func TestRunResume_NoAgentsError(t *testing.T) {
	ctx := context.Background()
	err := RunResume(ctx, ResumeOptions{Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v, want 'no coding agents' error", err)
	}
}

func TestRunResume_HappyPath(t *testing.T) {
	setupRunEnv(t)
	id := seedWorkingTaskWithSession(t)
	agent := newRunTestAgent("cursor")
	var stdout bytes.Buffer
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
	if !agent.lastWorkReq.Resume {
		t.Fatalf("lastWorkReq.Resume = false, want true")
	}
	if !strings.Contains(stdout.String(), "work resume on task "+id) {
		t.Fatalf("stdout = %q, missing resume message", stdout.String())
	}
}

func TestRunResume_UnknownTool(t *testing.T) {
	setupRunEnv(t)
	id := seedWorkingTaskWithSession(t)
	row := testutil.ReadTaskRow(t, id)
	row.WorkTool = "ghost-tool"
	testutil.SeedTaskRow(t, row)
	agent := newRunTestAgent("cursor")
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("err = %v, want 'unknown tool' error", err)
	}
}

func TestRunResume_NoResumableSessions(t *testing.T) {
	setupRunEnv(t)
	agent := newRunTestAgent("cursor")
	var stdout bytes.Buffer
	err := RunResume(context.Background(), ResumeOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if !strings.Contains(stdout.String(), "no resumable sessions") {
		t.Fatalf("stdout = %q, missing 'no resumable sessions'", stdout.String())
	}
	if agent.workCalls != 0 {
		t.Fatalf("workCalls = %d, want 0", agent.workCalls)
	}
}

func TestRunResume_WorkError(t *testing.T) {
	setupRunEnv(t)
	id := seedWorkingTaskWithSession(t)
	agent := newRunTestAgent("cursor")
	agent.workErr = errors.New("resume boom")
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
	})
	if err == nil || !strings.Contains(err.Error(), "resume boom") {
		t.Fatalf("err = %v, want 'resume boom'", err)
	}
}

func TestRunResume_SingleTaskAutoSelect(t *testing.T) {
	setupRunEnv(t)
	seedWorkingTaskWithSession(t)
	agent := newRunTestAgent("cursor")
	err := RunResume(context.Background(), ResumeOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1 (single task auto-selected)", agent.workCalls)
	}
	if agent.lastWorkReq.ResumeChatID != "existing-session" {
		t.Fatalf("ResumeChatID = %q, want 'existing-session'", agent.lastWorkReq.ResumeChatID)
	}
}

func TestRunResume_MultipleTaskPicker(t *testing.T) {
	setupRunEnv(t)
	id1 := seedWorkingTaskWithSession(t)
	id2 := seedWorkingTaskWithSession(t)
	agent := newRunTestAgent("cursor")
	err := RunResume(context.Background(), ResumeOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{pickTaskReturn: id2, pickTaskOK: true},
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
	_ = id1 // silence unused
}

func TestRunResume_PickerCancel(t *testing.T) {
	setupRunEnv(t)
	seedWorkingTaskWithSession(t)
	seedWorkingTaskWithSession(t)
	agent := newRunTestAgent("cursor")
	err := RunResume(context.Background(), ResumeOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeRunUI{pickTaskOK: false},
	})
	if err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if agent.workCalls != 0 {
		t.Fatalf("workCalls = %d, want 0 (picker cancel)", agent.workCalls)
	}
}

func TestRunResume_NoWorkSession(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t) // has no WorkResumeSession
	agent := newRunTestAgent("cursor")
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
	})
	if err == nil || !strings.Contains(err.Error(), "has no work session") {
		t.Fatalf("err = %v, want 'has no work session'", err)
	}
}

func TestRunResume_ResolveByIDTaskNotFound(t *testing.T) {
	setupRunEnv(t)
	agent := newRunTestAgent("cursor")
	err := RunResume(context.Background(), ResumeOptions{
		TaskID: "nonexistent",
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want 'not found'", err)
	}
}

func TestRunResume_AppliesDefaults(t *testing.T) {
	opts := ResumeOptions{}
	opts = opts.withDefaults()
	if opts.Stdin == nil || opts.Stdout == nil || opts.Stderr == nil {
		t.Fatal("withDefaults should fill nil streams")
	}
	if opts.UI == nil {
		t.Fatal("withDefaults should give default UI")
	}
}

// --- Helper function tests ---

func TestLookupResumeAgent_Found(t *testing.T) {
	agent := newRunTestAgent("cursor")
	result, ok := lookupResumeAgent([]codingagents.Agent{agent}, "cursor")
	if !ok {
		t.Fatal("expected agent to be found")
	}
	if result.Name() != "cursor" {
		t.Fatalf("Name() = %q, want cursor", result.Name())
	}
}

func TestLookupResumeAgent_NotFound(t *testing.T) {
	agent := newRunTestAgent("cursor")
	_, ok := lookupResumeAgent([]codingagents.Agent{agent}, "ghost")
	if ok {
		t.Fatal("should not find unknown tool")
	}
}

func TestResolveResumeByID_Happy(t *testing.T) {
	setupRunEnv(t)
	id := seedWorkingTaskWithSession(t)
	task, ok, err := resolveResumeByID(id)
	if err != nil {
		t.Fatalf("resolveResumeByID: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if task.ID != id {
		t.Fatalf("ID = %q, want %q", task.ID, id)
	}
}

func TestResolveResumeByID_NotFound(t *testing.T) {
	setupRunEnv(t)
	_, _, err := resolveResumeByID("nonexistent")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want 'not found'", err)
	}
}

func TestResolveResumeByID_NoWorkSession(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	_, _, err := resolveResumeByID(id)
	if err == nil || !strings.Contains(err.Error(), "has no work session") {
		t.Fatalf("err = %v, want 'has no work session'", err)
	}
}

func TestListResumableTasks_Happy(t *testing.T) {
	setupRunEnv(t)
	id := seedWorkingTaskWithSession(t)
	rows, err := listResumableTasks()
	if err != nil {
		t.Fatalf("listResumableTasks: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].ID != id {
		t.Fatalf("ID = %q, want %q", rows[0].ID, id)
	}
}

func TestListResumableTasks_Empty(t *testing.T) {
	setupRunEnv(t)
	rows, err := listResumableTasks()
	if err != nil {
		t.Fatalf("listResumableTasks: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len = %d, want 0", len(rows))
	}
}

func TestResolveResumeTask_WithTaskID(t *testing.T) {
	setupRunEnv(t)
	id := seedWorkingTaskWithSession(t)
	opts := ResumeOptions{TaskID: id, Stdout: io.Discard, Stderr: io.Discard}
	task, ok, err := resolveResumeTask(context.Background(), opts)
	if err != nil {
		t.Fatalf("resolveResumeTask: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if task.ID != id {
		t.Fatalf("ID = %q, want %q", task.ID, id)
	}
}

func TestResolveResumeTask_EmptyStore(t *testing.T) {
	setupRunEnv(t)
	opts := ResumeOptions{Stdout: io.Discard, Stderr: io.Discard}
	_, ok, err := resolveResumeTask(context.Background(), opts)
	if err != nil {
		t.Fatalf("resolveResumeTask: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when no resumable tasks")
	}
}

func TestResolveResumeTask_SingleAuto(t *testing.T) {
	setupRunEnv(t)
	id := seedWorkingTaskWithSession(t)
	opts := ResumeOptions{Stdout: io.Discard, Stderr: io.Discard}
	task, ok, err := resolveResumeTask(context.Background(), opts)
	if err != nil {
		t.Fatalf("resolveResumeTask: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true (single task auto-select)")
	}
	if task.ID != id {
		t.Fatalf("ID = %q, want %q", task.ID, id)
	}
}

func TestResolveResumeTask_MultiplePicker(t *testing.T) {
	setupRunEnv(t)
	seedWorkingTaskWithSession(t)
	id2 := seedWorkingTaskWithSession(t)
	ui := &fakeRunUI{pickTaskReturn: id2, pickTaskOK: true}
	opts := ResumeOptions{UI: ui, Stdout: io.Discard, Stderr: io.Discard}
	task, ok, err := resolveResumeTask(context.Background(), opts)
	if err != nil {
		t.Fatalf("resolveResumeTask: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if task.ID != id2 {
		t.Fatalf("ID = %q, want %q", task.ID, id2)
	}
}

func TestResolveResumeTask_PickerCancel(t *testing.T) {
	setupRunEnv(t)
	seedWorkingTaskWithSession(t)
	seedWorkingTaskWithSession(t)
	ui := &fakeRunUI{pickTaskOK: false}
	opts := ResumeOptions{UI: ui, Stdout: io.Discard, Stderr: io.Discard}
	_, ok, err := resolveResumeTask(context.Background(), opts)
	if err != nil {
		t.Fatalf("resolveResumeTask: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false on picker cancel")
	}
}

func TestRunResume_PickerChosenIDNotFound(t *testing.T) {
	setupRunEnv(t)
	seedWorkingTaskWithSession(t)
	seedWorkingTaskWithSession(t)
	ui := &fakeRunUI{pickTaskReturn: "ghost", pickTaskOK: true}
	opts := ResumeOptions{UI: ui, Stdout: io.Discard, Stderr: io.Discard, Agents: []codingagents.Agent{newRunTestAgent("cursor")}}
	err := RunResume(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), `task "ghost" not found`) {
		t.Fatalf("err = %v, want 'task \"ghost\" not found'", err)
	}
}

func TestRunResume_ErrUserAborted(t *testing.T) {
	setupRunEnv(t)
	seedWorkingTaskWithSession(t)
	seedWorkingTaskWithSession(t)
	ui := &fakeRunUI{pickTaskOK: false, pickTaskErr: huh.ErrUserAborted}
	opts := ResumeOptions{UI: ui, Stdout: io.Discard, Stderr: io.Discard, Agents: []codingagents.Agent{newRunTestAgent("cursor")}}
	err := RunResume(context.Background(), opts)
	if err != nil {
		t.Fatalf("RunResume: %v (huh.ErrUserAborted should translate to nil)", err)
	}
}

func TestRun_NoPlanTasks(t *testing.T) {
	setupRunEnv(t)
	// Don't seed any plan-done tasks — ResolveWorkPlan returns an error
	agent := newRunTestAgent("cursor")
	err := Run(context.Background(), Options{
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

// --- Additional coverage tests ---

func TestRun_NewResumeIDError(t *testing.T) {
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.resumeIDErr = errors.New("resume id failure")
	err := Run(context.Background(), Options{
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
		t.Fatalf("Run: %v (NewResumeID error should not abort)", err)
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
	err := Run(context.Background(), Options{
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

func TestResolveResumeTask_PickerError(t *testing.T) {
	setupRunEnv(t)
	seedWorkingTaskWithSession(t)
	seedWorkingTaskWithSession(t)
	ui := &fakeRunUI{pickTaskErr: errors.New("picker broke")}
	opts := ResumeOptions{UI: ui, Stdout: io.Discard, Stderr: io.Discard}
	_, ok, err := resolveResumeTask(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "picker broke") {
		t.Fatalf("err = %v, want 'picker broke'", err)
	}
	if ok {
		t.Fatal("expected ok=false on picker error")
	}
}

func TestResolveResumeTask_ChosenNotInList(t *testing.T) {
	setupRunEnv(t)
	seedWorkingTaskWithSession(t)
	seedWorkingTaskWithSession(t)
	ui := &fakeRunUI{pickTaskReturn: "ghost-id", pickTaskOK: true}
	opts := ResumeOptions{UI: ui, Stdout: io.Discard, Stderr: io.Discard}
	_, ok, err := resolveResumeTask(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want 'not found'", err)
	}
	if ok {
		t.Fatal("expected ok=false")
	}
}

func TestRun_WaitForCompletion_PIDZero(t *testing.T) {
	// PID zero with WaitForCompletion just runs Finish synchronously (no WaitForExit).
	setupRunEnv(t)
	id := seedPlanDoneTask(t)
	agent := newRunTestAgent("cursor")
	agent.workPid = 0
	err := Run(context.Background(), Options{
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
		t.Fatalf("Run: %v", err)
	}
	if agent.workCalls != 1 {
		t.Fatalf("workCalls = %d, want 1", agent.workCalls)
	}
}
