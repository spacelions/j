package tasks

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

	"github.com/charmbracelet/huh"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/testutil"
)

// continueAgent is the dispatch fake used by RunContinue tests. It
// records which phase methods were called so tests can assert
// dispatchByStatus routes to the right one without re-implementing
// the downstream lifecycle. The fake also writes the side-effect
// files real agents would (requirements.md / plan.md / verifier_*)
// so the orchestration helpers downstream succeed.
type continueAgent struct {
	scriptedAgent

	planned   int
	worked    int
	verified  int
	planReq   codingagents.PlanRequest
	workReq   codingagents.WorkRequest
	verifyReq codingagents.VerifyRequest
}

func newContinueAgent() *continueAgent {
	return &continueAgent{scriptedAgent: scriptedAgent{
		name:   "cursor",
		models: []string{"sonnet-4"},
	}}
}

func (a *continueAgent) NewResumeID(context.Context) (string, error) {
	return "00000000-0000-4000-8000-000000000001", nil
}

func (a *continueAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.planned++
	a.planReq = req
	if req.RequirementsOutputPath != "" {
		body, _ := os.ReadFile(req.FromFilePath)
		_ = os.WriteFile(req.RequirementsOutputPath, body, 0o644)
	}
	if req.PlanOutputPath != "" {
		_ = os.WriteFile(req.PlanOutputPath, []byte("1. step\n"), 0o644)
	}
	return 0, nil
}

func (a *continueAgent) Work(_ context.Context, req codingagents.WorkRequest) (int, error) {
	a.worked++
	a.workReq = req
	return 0, nil
}

func (a *continueAgent) Verify(_ context.Context, req codingagents.VerifyRequest) (int, error) {
	a.verified++
	a.verifyReq = req
	if req.VerifierFindingsOutputPath != "" {
		_ = os.WriteFile(req.VerifierFindingsOutputPath, []byte("VERDICT: PASS\n"), 0o644)
	}
	if req.VerifierPlanOutputPath != "" {
		_ = os.WriteFile(req.VerifierPlanOutputPath, []byte("# verifier plan\n"), 0o644)
	}
	return 0, nil
}

// seedTaskFull writes a task row plus its requirements.md / plan.md
// files. The mutate hook lets each test override fields. The agent
// buckets are pre-populated so RunContinue's EnsureAgentSelections
// skips its prompt path.
func seedTaskFull(t *testing.T, mutate func(*store.Task)) string {
	t.Helper()
	id := store.NewTaskID()
	taskDir, err := store.EnsureTaskDir(id)
	if err != nil {
		t.Fatalf("EnsureTaskDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, store.RequirementsFileName), []byte("# req\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, store.PlanFileName), []byte("1. step\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	begin := time.Now().UTC().Add(-2 * time.Hour)
	end := begin.Add(time.Hour)
	task := store.Task{
		ID:               id,
		Status:           store.StatusPlanDone,
		InvokedTool:      "cursor",
		InvokedModel:     "sonnet-4",
		PlanResumeCursor: "plan-cursor",
		Summary:          "seed",
		PlanBeginAt:      &begin,
		PlanEndAt:        &end,
	}
	if mutate != nil {
		mutate(&task)
	}
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		t.Fatal(err)
	}
	return id
}

func setupContinueEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		seedAgentBucket(t, bucket, "cursor", "sonnet-4")
	}
}

// TestRunContinue_PlanningDispatchesToPlanResume pins planning -> plan.RunResume.
func TestRunContinue_PlanningDispatchesToPlanResume(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusPlanning
	})
	agent := newContinueAgent()
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned != 1 {
		t.Fatalf("planned = %d, want 1", agent.planned)
	}
	if !agent.planReq.Resume {
		t.Fatalf("Resume = false, want true (plan.RunResume should set it)")
	}
	if agent.worked != 0 || agent.verified != 0 {
		t.Fatalf("dispatched to wrong phase: worked=%d verified=%d", agent.worked, agent.verified)
	}
}

// TestRunContinue_PlanDoneSpawnsOrchestrator pins plan-done ->
// detached `j tasks orchestrate --skip-planning=true
// --plan-requires-approval=false`. The stub J binary records its
// argv so we can assert the spawn arguments; PID + AgentLogPath are
// stamped on the row.
func TestRunContinue_PlanDoneSpawnsOrchestrator(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil) // default is plan-done
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	binary := argvJBinary(t, argvPath)
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: binary,
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call should fire (spawned child runs the chain): planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{"tasks", "orchestrate", "--id", id, "--plan-requires-approval=false", "--skip-planning=true"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
	row := readTaskFromBolt(t, id)
	if row.BackgroundPID == 0 {
		t.Fatalf("BackgroundPID = 0; want non-zero detached child PID")
	}
	wantLog := filepath.Join(".j/tasks", id, store.AgentLogFileName)
	if !strings.HasSuffix(row.AgentLogPath, wantLog) {
		t.Fatalf("AgentLogPath = %q, want suffix %q", row.AgentLogPath, wantLog)
	}
	if !strings.Contains(stdout.String(), "task "+id+" running in background") || !strings.Contains(stdout.String(), "tail -f") {
		t.Fatalf("stdout = %q, want background-fork announcement", stdout.String())
	}
}

// TestRunContinue_PlanDoneSpawnFails pins SpawnIn-error propagation:
// pointing JBinary at a missing path surfaces the spawn error
// verbatim with no row mutation.
func TestRunContinue_PlanDoneSpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	agent := newContinueAgent()
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: "/no/such/binary-xyzzy",
	})
	if err == nil {
		t.Fatal("expected spawn failure")
	}
	row := readTaskFromBolt(t, id)
	if row.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (no row mutation on spawn failure)", row.BackgroundPID)
	}
}

// TestRunContinue_WorkingDispatchesToWorkResume pins working -> work.RunResume.
func TestRunContinue_WorkingDispatchesToWorkResume(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusWorking
		task.WorkResumeCursor = "work-cursor"
	})
	agent := newContinueAgent()
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.worked != 1 {
		t.Fatalf("worked = %d, want 1", agent.worked)
	}
	if !agent.workReq.Resume {
		t.Fatalf("workReq.Resume = false, want true")
	}
}

// TestRunContinue_WorkDoneDispatchesToVerify pins work-done -> verify.Run.
func TestRunContinue_WorkDoneDispatchesToVerify(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusWorkDone
		task.WorkResumeCursor = "work-cursor"
	})
	agent := newContinueAgent()
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.verified != 1 {
		t.Fatalf("verified = %d, want 1", agent.verified)
	}
	if agent.verifyReq.Resume {
		t.Fatalf("verify.Run should not set Resume on first dispatch")
	}
}

// TestRunContinue_VerifyingDispatchesToVerifyResume pins verifying -> verify.RunResume.
func TestRunContinue_VerifyingDispatchesToVerifyResume(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusVerifying
		task.VerifyResumeCursor = "verify-cursor"
	})
	agent := newContinueAgent()
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.verified != 1 {
		t.Fatalf("verified = %d, want 1", agent.verified)
	}
	if !agent.verifyReq.Resume {
		t.Fatalf("verify.RunResume should set Resume=true")
	}
}

// TestRunContinue_VerifyDoneShortCircuits pins verify-done -> done message,
// no dispatch, exit 0.
func TestRunContinue_VerifyDoneShortCircuits(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusVerifyDone
	})
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no agent should run for verify-done; got planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	want := "J: task " + id + " already finished"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
	}
}

// TestRunContinue_CompletedShortCircuits mirrors verify-done for the
// completed status.
func TestRunContinue_CompletedShortCircuits(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusCompleted
	})
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	want := "already finished"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
	}
}

// TestRunContinue_HelpFromVerifyEnd pins help-status dispatch when
// VerifyEndAt is the freshest timestamp: routes to verify.RunResume.
func TestRunContinue_HelpFromVerifyEnd(t *testing.T) {
	setupContinueEnv(t)
	t1 := time.Now().UTC().Add(-3 * time.Hour)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusHelp
		task.VerifyResumeCursor = "verify-cursor"
		task.WorkResumeCursor = "work-cursor"
		task.PlanEndAt = &t1
		task.WorkEndAt = &t2
		task.VerifyEndAt = &t3
	})
	agent := newContinueAgent()
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.verified != 1 {
		t.Fatalf("verified = %d, want 1 (help with VerifyEndAt should resume verify)", agent.verified)
	}
}

// TestRunContinue_HelpFromWorkEnd: help with only WorkEndAt set ->
// work.RunResume.
func TestRunContinue_HelpFromWorkEnd(t *testing.T) {
	setupContinueEnv(t)
	t1 := time.Now().UTC().Add(-2 * time.Hour)
	t2 := t1.Add(time.Hour)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusHelp
		task.WorkResumeCursor = "work-cursor"
		task.PlanEndAt = &t1
		task.WorkEndAt = &t2
	})
	agent := newContinueAgent()
	if err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.worked != 1 {
		t.Fatalf("worked = %d, want 1", agent.worked)
	}
}

// TestRunContinue_HelpFromPlanEnd: help with only PlanEndAt set ->
// plan.RunResume.
func TestRunContinue_HelpFromPlanEnd(t *testing.T) {
	setupContinueEnv(t)
	t1 := time.Now().UTC().Add(-2 * time.Hour)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusHelp
		task.PlanResumeCursor = "plan-cursor"
		task.PlanEndAt = &t1
	})
	agent := newContinueAgent()
	if err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned != 1 {
		t.Fatalf("planned = %d, want 1", agent.planned)
	}
}

// TestRunContinue_HelpFromCursorFallback covers the cursor-precedence
// fallback when no *EndAt is set: WorkResumeCursor wins over plan.
func TestRunContinue_HelpFromCursorFallback(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusHelp
		task.PlanEndAt = nil
		task.PlanResumeCursor = "plan-cursor"
		task.WorkResumeCursor = "work-cursor"
	})
	agent := newContinueAgent()
	if err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.worked != 1 {
		t.Fatalf("worked = %d, want 1 (work cursor wins over plan)", agent.worked)
	}
}

// TestRunContinue_HelpNoSignal pins the error path: a help row with no
// *EndAt timestamps and every resume cursor empty cannot be dispatched.
func TestRunContinue_HelpNoSignal(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusHelp
		task.PlanEndAt = nil
		task.PlanResumeCursor = ""
		task.WorkResumeCursor = ""
		task.VerifyResumeCursor = ""
	})
	agent := newContinueAgent()
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "no resumable phase signal") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunContinue_MissingTaskFromFlag pins --from-task pointing at an
// unknown id: prints `J: no task` and exits 0 (no dispatch).
func TestRunContinue_MissingTaskFromFlag(t *testing.T) {
	setupContinueEnv(t)
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: "ghost-id",
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no dispatch should fire on missing id")
	}
	if !strings.Contains(stdout.String(), noTaskMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), noTaskMessage)
	}
}

// TestRunContinue_PickerCancel pins user-cancel from the picker:
// returns nil with no dispatch.
func TestRunContinue_PickerCancel(t *testing.T) {
	setupContinueEnv(t)
	seedTaskFull(t, nil)
	seedTaskFull(t, nil)
	agent := newContinueAgent()
	if err := RunContinue(context.Background(), ContinueOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{}, // empty pickReturn -> picker returns "" -> cancel
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no dispatch should fire on cancel")
	}
}

// TestRunContinue_PickerHappy pins the no-flag picker path: the user
// selects one row and dispatch fires for it. plan-done routes to a
// detached spawn, so we point JBinary at a stub and assert the spawn
// fired (no in-process agent call expected).
func TestRunContinue_PickerHappy(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil) // plan-done -> detached spawn
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	ui := &fakeUI{pickReturn: id}
	if err := RunContinue(context.Background(), ContinueOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	args := readSpawnedArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate ...`", args)
	}
}

// TestRunContinue_NoTasksFile pins the missing-tasks-db short-circuit:
// no list.db, no --from-task -> emptyMessage and exit 0.
func TestRunContinue_NoTasksFile(t *testing.T) {
	t.Chdir(t.TempDir())
	// Do NOT call mustInit so list.db never gets created.
	agent := newContinueAgent()
	var stdout bytes.Buffer
	if err := RunContinue(context.Background(), ContinueOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if !strings.Contains(stdout.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), emptyMessage)
	}
}

// TestRunContinue_NoAgents pins the no-agents-configured branch.
func TestRunContinue_NoAgents(t *testing.T) {
	err := RunContinue(context.Background(), ContinueOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunContinue_AgentSelectorAborts pins the deferred huh.ErrUserAborted
// guard from EnsureAgentSelections: a Ctrl-C in the selector exits
// cleanly with no dispatch.
func TestRunContinue_AgentSelectorAborts(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	id := seedTaskFull(t, nil)
	// No agent buckets seeded so EnsureAgentSelections prompts.
	agent := newContinueAgent()
	if err := RunContinue(context.Background(), ContinueOptions{
		TaskID:   id,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{agent},
		UI:       &fakeUI{},
		Selector: &testutil.SelectorFake{ToolErr: huh.ErrUserAborted},
	}); err != nil {
		t.Fatalf("err = %v, want nil (abort exits cleanly)", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no dispatch should fire after abort")
	}
}

// TestRunContinue_AppliesDefaults exercises ContinueOptions.withDefaults.
func TestRunContinue_AppliesDefaults(t *testing.T) {
	setupContinueEnv(t)
	if err := RunContinue(context.Background(), ContinueOptions{
		Agents: []codingagents.Agent{newContinueAgent()},
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
}

// TestLatestPhase pins the precedence rules in isolation. Three
// matrices: end-timestamps, cursor-fallback, no-signal.
func TestLatestPhase(t *testing.T) {
	t1 := time.Now().UTC().Add(-3 * time.Hour)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	cases := []struct {
		name string
		task store.Task
		want string
	}{
		{"verify-end-wins", store.Task{PlanEndAt: &t1, WorkEndAt: &t2, VerifyEndAt: &t3}, "verify"},
		{"work-end-wins", store.Task{PlanEndAt: &t1, WorkEndAt: &t3}, "work"},
		{"plan-end-only", store.Task{PlanEndAt: &t1}, "plan"},
		{"verify-cursor-fallback", store.Task{VerifyResumeCursor: "v"}, "verify"},
		{"work-cursor-fallback", store.Task{WorkResumeCursor: "w"}, "work"},
		{"plan-cursor-fallback", store.Task{PlanResumeCursor: "p"}, "plan"},
		{"no-signal", store.Task{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := latestPhase(c.task); got != c.want {
				t.Fatalf("latestPhase = %q, want %q", got, c.want)
			}
		})
	}
}

// TestNewContinueCmd_FlagDefaults pins the registered flag set,
// defaults, and viper bindings for `j tasks continue`.
func TestNewContinueCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newContinueCmd()
	if cmd.Use != "continue" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	if len(names) != 1 || names[0] != "from-task" {
		t.Fatalf("flags = %v, want only [from-task]", names)
	}
}

// TestNewContinueCmd_FlagsBindToViper covers --from-task viper bind.
func TestNewContinueCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newContinueCmd()
	if err := cmd.Flags().Set("from-task", "abc"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("tasks.continue.from_task"); got != "abc" {
		t.Errorf("tasks.continue.from_task = %q", got)
	}
}

// TestNewContinueCmd_EnvBinding covers TASKS_CONTINUE_FROM_TASK.
func TestNewContinueCmd_EnvBinding(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_CONTINUE_FROM_TASK", "env-id")
	_ = newContinueCmd()
	if got := viper.GetString("tasks.continue.from_task"); got != "env-id" {
		t.Errorf("tasks.continue.from_task = %q", got)
	}
}

// TestNewContinueCmd_RunE_MissingTask exercises the RunE closure end
// to end. With no list.db on disk, the command short-circuits with
// the empty message and returns nil; this proves the closure
// constructed ContinueOptions and reached RunContinue.
func TestNewContinueCmd_RunE_MissingTask(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	cmd := newContinueCmd()
	cmd.SetContext(context.Background())
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(stdout.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), emptyMessage)
	}
}

// TestRunContinue_RegisteredAsChild verifies wiring on the parent.
func TestRunContinue_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "continue" {
			return
		}
	}
	t.Fatal("`j tasks continue` should be registered as a child of `j tasks`")
}

// TestRunContinue_StoreOpenError covers the store-open failure branch
// in resolveContinueTask.
func TestRunContinue_StoreOpenError(t *testing.T) {
	t.Chdir(t.TempDir())
	mustInit(t)
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	agent := newContinueAgent()
	err = RunContinue(context.Background(), ContinueOptions{
		TaskID: "any",
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err == nil {
		t.Fatal("expected error when store path is a directory")
	}
}

// TestRunContinue_GetTaskDecodeError plants malformed JSON under an id
// so resolveContinueTaskFromStore -> GetTask returns a non-fs.ErrNotExist
// error.
func TestRunContinue_GetTaskDecodeError(t *testing.T) {
	setupContinueEnv(t)
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBucket(store.BucketTasks); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(store.BucketTasks, "broken", "not-json"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	err = RunContinue(context.Background(), ContinueOptions{
		TaskID: "broken",
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     &fakeUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "decode task") {
		t.Fatalf("err = %v", err)
	}
}

// TestDispatchByStatus_UnknownStatus covers the safety-net branch.
func TestDispatchByStatus_UnknownStatus(t *testing.T) {
	err := dispatchByStatus(context.Background(), ContinueOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
	}, store.Task{ID: "x", Status: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "unsupported status") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunContinue_DispatchPlanError exercises the error-propagation
// path: dispatch routes to plan.RunResume but the agent returns an
// error.
func TestRunContinue_DispatchPlanError(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *store.Task) {
		task.Status = store.StatusPlanning
	})
	agent := &errPlanContinueAgent{
		continueAgent: *newContinueAgent(),
		planErr:       errors.New("plan boom"),
	}
	err := RunContinue(context.Background(), ContinueOptions{
		TaskID: id,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{agent},
		UI:     &fakeUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "plan boom") {
		t.Fatalf("err = %v", err)
	}
}

type errPlanContinueAgent struct {
	continueAgent
	planErr error
}

func (a *errPlanContinueAgent) Plan(_ context.Context, req codingagents.PlanRequest) (int, error) {
	a.continueAgent.planned++
	a.continueAgent.planReq = req
	return 0, a.planErr
}

// TestResumeFromPlanDone_ArgvParsesThroughOrchestrateCmd is the
// regression guard for the pflag two-token bool bug: even if the
// argv-shape assertion above is reverted, the spawned argv must
// still parse through a real `j tasks orchestrate` cobra command
// with plan-requires-approval=false and skip-planning=true. Catches
// any future revert to the `"--flag", "value"` shape because pflag
// would mark the bool flag Changed=true (no `=` consumes the next
// token as a positional, leaving the bool at its default true) and
// fail the assertion below.
func TestResumeFromPlanDone_ArgvParsesThroughOrchestrateCmd(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	if err := RunContinue(context.Background(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	if len(args) < 2 || args[0] != "tasks" || args[1] != "orchestrate" {
		t.Fatalf("argv = %v, want leading `tasks orchestrate`", args)
	}
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newOrchestrateCmd()
	if err := cmd.ParseFlags(args[2:]); err != nil {
		t.Fatalf("ParseFlags(%v): %v", args[2:], err)
	}
	if !cmd.Flags().Changed("plan-requires-approval") {
		t.Fatalf("plan-requires-approval not Changed; argv=%v", args)
	}
	approval, err := cmd.Flags().GetBool("plan-requires-approval")
	if err != nil {
		t.Fatalf("GetBool plan-requires-approval: %v", err)
	}
	if approval {
		t.Fatalf("plan-requires-approval = true, want false; argv=%v", args)
	}
	skip, err := cmd.Flags().GetBool("skip-planning")
	if err != nil {
		t.Fatalf("GetBool skip-planning: %v", err)
	}
	if !skip {
		t.Fatalf("skip-planning = false, want true; argv=%v", args)
	}
}

// TestRunContinue_BboltLockedSuppressesBanner pins the resume-after-
// plan-done timeout-suppression contract. With another open handle
// holding the bbolt lock, stampSpawnOnRow must emit the refined
// `■ J: cannot write to database` line and return store.ErrOpenTimeout
// so resumeFromPlanDone can suppress the misleading
// RunningInBackground banner. RunContinue is then driven end to end
// against the same lock to assert (a) the refined banner reaches
// stderr from resolveContinueTask itself (which races on the lock
// before stampSpawnOnRow is ever reached), (b) RunContinue exits
// cleanly without leaking the legacy `store: open … timeout` wording,
// and (c) no `RunningInBackground` banner reaches stdout.
func TestRunContinue_BboltLockedSuppressesBanner(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil) // plan-done by default
	dbPath, err := store.DefaultTasksDBPath()
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open(holder): %v", err)
	}
	t.Cleanup(func() { _ = holder.Close() })

	var stampStderr bytes.Buffer
	stampErr := stampSpawnOnRow(&stampStderr, id, filepath.Join(t.TempDir(), "agent.log"), 4242)
	if !errors.Is(stampErr, store.ErrOpenTimeout) {
		t.Fatalf("stampSpawnOnRow err = %v, want ErrOpenTimeout", stampErr)
	}
	if !strings.Contains(stampStderr.String(), "■ J: cannot write to database") {
		t.Fatalf("stampSpawnOnRow stderr = %q, want refined timeout banner", stampStderr.String())
	}
	if strings.Contains(stampStderr.String(), "warning: tasks db") {
		t.Fatalf("stampSpawnOnRow stderr should not contain legacy warning: %q", stampStderr.String())
	}

	var stdout, stderr bytes.Buffer
	if err := RunContinue(context.Background(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  &stderr,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      &fakeUI{},
		JBinary: noopJBinary(t),
	}); err != nil {
		t.Fatalf("RunContinue under lock should return nil after refined banner; got %v", err)
	}
	if !strings.Contains(stderr.String(), "■ J: cannot write to database") {
		t.Fatalf("RunContinue stderr = %q, want refined timeout banner", stderr.String())
	}
	if strings.Contains(stderr.String(), "store: open") {
		t.Fatalf("RunContinue stderr should not leak legacy `store: open` wording: %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "running in background") {
		t.Fatalf("stdout should not announce a background banner under lock contention: %q", stdout.String())
	}
}
