package tasks

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// continueAgent is the dispatch fake used by RunContinue tests. It
// records which phase methods were called so tests can assert
// dispatchByStatus routes to the right one without re-implementing
// the downstream lifecycle. The fake also writes the side-effect
// files real agents would (requirements.md / plan.md / verifier_*)
// so the orchestration helpers downstream succeed.
type continueAgent struct {
	testutil.ScriptedAgent

	planned   int
	worked    int
	verified  int
	planReq   codingagents.PlanRequest
	workReq   codingagents.WorkRequest
	verifyReq codingagents.VerifyRequest
}

func newContinueAgent() *continueAgent {
	return &continueAgent{ScriptedAgent: testutil.ScriptedAgent{
		AgentName: "cursor",
		Models:    []string{"sonnet-4"},
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

func setupContinueEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	mustInit(t)
	for _, bucket := range []string{store.BucketPlanner, store.BucketWorker, store.BucketVerifier} {
		testutil.SeedAgentBucketToolModel(t, bucket, "cursor", "sonnet-4")
	}
}

// installCursorAgentLoginStub drops a PATH-resolvable `cursor-agent`
// shell script that prints "Logged in" and exits 0 so
// cursor.Agent.CheckLogin succeeds without the real binary. The stub
// ignores argv, so it covers the `status` subcommand and any future
// caller. Only the four PreRunE tests need it, so we wire it per-test
// rather than from setupContinueEnv to keep the helper localized.
func installCursorAgentLoginStub(t *testing.T) {
	t.Helper()
	testutil.InstallCursorAgentLoginStub(t)
}

// TestRunContinue_PlanningShowsTooltip pins planning -> tooltip message
// suggesting `j tasks re-plan` or `j tasks resume-plan`.
func TestRunContinue_PlanningShowsTooltip(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusPlanning
	})
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(t.Context(), ContinueOptions{
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
		t.Fatalf("no in-process agent call should fire for planning status: planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	want := "use `j tasks resume-plan`"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
	}
}

// TestRunContinue_PlanDoneDispatchesToOrchestratorFromWork pins
// plan-done -> detached `j tasks orchestrate --phase=from-work` so the
// orchestrator's worker → verifier chain runs to a terminal status.
// The in-process worker no longer fires from this path.
func TestRunContinue_PlanDoneDispatchesToOrchestratorFromWork(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil) // default is plan-done
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call (spawned child runs the chain): planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{
		"tasks", "orchestrate",
		"--id", id,
		"--phase=from-work",
		"--interactive=false",
	}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
	_ = readTaskFromBolt(t, id)
}

// TestRunContinue_PlanDoneForwardsToolModel pins that --tool / --model
// flags forward into the orchestrate argv on the plan-done dispatch
// path so the child's preflight surfaces tool problems.
func TestRunContinue_PlanDoneForwardsToolModel(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil)
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Tool:    "claude",
		Model:   "opus-4",
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	if !containsArg(args, "--tool=claude") {
		t.Fatalf("argv = %v, want --tool=claude", args)
	}
	if !containsArg(args, "--model=opus-4") {
		t.Fatalf("argv = %v, want --model=opus-4", args)
	}
	if !containsArg(args, "--phase=from-work") {
		t.Fatalf("argv = %v, want --phase=from-work", args)
	}
}

// TestRunContinue_PlanDoneSpawnFails covers the error path when the
// detached orchestrator spawn fails (binary missing).
func TestRunContinue_PlanDoneSpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil)
	err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      &fakeUI{},
		JBinary: "/no/such/binary-xyzzy",
	})
	if err == nil {
		t.Fatal("expected spawn failure")
	}
}

func TestRunPlanDoneWork_EnsureDirError(t *testing.T) {
	t.Chdir(t.TempDir())
	err := runPlanDoneWork(
		t.Context(),
		ContinueOptions{Stdout: io.Discard, Stderr: io.Discard},
		tasks.Task{ID: "missing-layout"},
	)
	if err == nil || !strings.Contains(err.Error(), "ensure task dir") {
		t.Fatalf("err = %v, want ensure task dir", err)
	}
}

// TestRunContinue_PlanDoneInlineWhenInteractive pins that
// --interactive=true selects the inline orchestrator path (the parent
// blocks on the child instead of forking).
func TestRunContinue_PlanDoneInlineWhenInteractive(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil)
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	err := RunContinue(t.Context(), ContinueOptions{
		TaskID:      id,
		Interactive: new(true),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{agent},
		UI:          &fakeUI{},
		JBinary:     argvJBinary(t, argvPath),
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	if !containsArg(args, "--interactive=true") {
		t.Fatalf("argv = %v, want --interactive=true", args)
	}
	if !containsArg(args, "--phase=from-work") {
		t.Fatalf("argv = %v, want --phase=from-work", args)
	}
	// Inline path runs synchronously; nothing further to assert here.
	_ = readTaskFromBolt(t, id)
}

// TestRunContinue_WorkingShowsTooltip pins working -> tooltip message
// suggesting `j tasks re-work` or `j tasks resume-work`.
func TestRunContinue_WorkingShowsTooltip(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorking
		task.WorkResumeSession = "work-cursor"
	})
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(t.Context(), ContinueOptions{
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
		t.Fatalf("no agent call should fire for working status: planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	want := "use `j tasks resume-work`"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
	}
}

// TestRunContinue_WorkDoneDispatchesToVerify pins work-done ->
// detached `j tasks orchestrate --phase=verify-only`.
func TestRunContinue_WorkDoneDispatchesToVerify(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorkDone
		task.WorkResumeSession = "work-cursor"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call should fire (spawned child runs the chain): planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{"tasks", "orchestrate", "--id", id, "--phase=verify-only"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
	_ = readTaskFromBolt(t, id)
}

// TestRunContinue_VerifyingDispatchesToVerifyResume pins verifying ->
// inline `j tasks orchestrate --phase=verify-only --interactive=true`.
// Uses noopJBinary because the inline path blocks until the process
// exits.
func TestRunContinue_VerifyingDispatchesToVerifyResume(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusVerifying
		task.VerifyResumeSession = "verify-cursor"
	})
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: noopJBinary(t),
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call should fire: planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
}

// TestRunContinue_FailedShortCircuits pins failed -> done message,
// no dispatch, exit 0.
func TestRunContinue_FailedShortCircuits(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
	})
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(t.Context(), ContinueOptions{
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
		t.Fatalf("no agent should run for failed; got planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	want := "J: task " + id + " already finished"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
	}
}

// TestRunContinue_CompletedShortCircuits mirrors failed for the
// completed status.
func TestRunContinue_CompletedShortCircuits(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
	})
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(t.Context(), ContinueOptions{
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
// VerifyEndAt is the freshest timestamp: re-execs the orchestrator
// with --phase=verify-only --interactive=true so the verifier resumes
// inline. Stubbing JBinary captures the spawned argv instead of
// running the real test binary recursively.
func TestRunContinue_HelpFromVerifyEnd(t *testing.T) {
	setupContinueEnv(t)
	t1 := time.Now().UTC().Add(-3 * time.Hour)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusHelp
		task.VerifyResumeSession = "verify-cursor"
		task.WorkResumeSession = "work-cursor"
		task.PlanEndAt = t1
		task.WorkEndAt = t2
		task.VerifyEndAt = t3
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	if err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call (spawned child): planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{"tasks", "orchestrate", "--id", id, "--phase=verify-only", "--interactive=true"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
}

// TestRunContinue_HelpFromWorkEnd: help with only WorkEndAt set ->
// detached `j tasks orchestrate --phase=from-work --interactive=true`.
func TestRunContinue_HelpFromWorkEnd(t *testing.T) {
	setupContinueEnv(t)
	t1 := time.Now().UTC().Add(-2 * time.Hour)
	t2 := t1.Add(time.Hour)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusHelp
		task.WorkResumeSession = "work-cursor"
		task.PlanEndAt = t1
		task.WorkEndAt = t2
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	if err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call should fire (spawned child): planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{"tasks", "orchestrate", "--id", id, "--phase=from-work", "--interactive=true"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
}

// TestRunContinue_HelpFromPlanEnd: help with only PlanEndAt set ->
// inline orchestrator with --plan-requires-approval=true --interactive=true.
func TestRunContinue_HelpFromPlanEnd(t *testing.T) {
	setupContinueEnv(t)
	t1 := time.Now().UTC().Add(-2 * time.Hour)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusHelp
		task.PlanResumeSession = "plan-cursor"
		task.PlanEndAt = t1
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	if err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call for help+plan dispatch: planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{"tasks", "orchestrate", "--id", id, "--plan-requires-approval=true", "--interactive=true"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
}

// TestRunContinue_HelpFromCursorFallback covers the cursor-precedence
// fallback when no *EndAt is set: WorkResumeSession wins over plan,
// spawning a detached orchestrator with --phase=from-work.
func TestRunContinue_HelpFromCursorFallback(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusHelp
		task.PlanEndAt = time.Time{}
		task.PlanResumeSession = "plan-cursor"
		task.WorkResumeSession = "work-cursor"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	if err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call: planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{"tasks", "orchestrate", "--id", id, "--phase=from-work", "--interactive=true"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
}

// TestRunContinue_HelpNoSignal pins the error path: a help row with no
// *EndAt timestamps and every resume cursor empty cannot be dispatched.
func TestRunContinue_HelpNoSignal(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusHelp
		task.PlanEndAt = time.Time{}
		task.PlanResumeSession = ""
		task.WorkResumeSession = ""
		task.VerifyResumeSession = ""
	})
	agent := newContinueAgent()
	err := RunContinue(t.Context(), ContinueOptions{
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
	err := RunContinue(t.Context(), ContinueOptions{
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
	testutil.SeedFullTask(t, nil)
	testutil.SeedFullTask(t, nil)
	agent := newContinueAgent()
	if err := RunContinue(t.Context(), ContinueOptions{
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
// selects one row and dispatch fires for it. plan-done now spawns the
// orchestrator detached, so the captured argv carries
// --phase=from-work rather than the agent.Work counter ticking.
func TestRunContinue_PickerHappy(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil) // plan-done -> detached orchestrator
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	ui := &fakeUI{pickReturn: id}
	if err := RunContinue(t.Context(), ContinueOptions{
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
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call (spawned child runs the chain): planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	if !containsArg(args, "--phase=from-work") {
		t.Fatalf("argv = %v, want --phase=from-work", args)
	}
}

// TestRunContinue_NoTasksFile pins the missing-tasks-db short-circuit:
// no list.db, no --from-task -> emptyMessage and exit 0.
func TestRunContinue_NoTasksFile(t *testing.T) {
	t.Chdir(t.TempDir())
	// Do NOT call mustInit so list.db never gets created.
	agent := newContinueAgent()
	var stdout bytes.Buffer
	if err := RunContinue(t.Context(), ContinueOptions{
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
	err := RunContinue(t.Context(), ContinueOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "no coding agents") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunContinue_AppliesDefaults exercises ContinueOptions.withDefaults.
func TestRunContinue_AppliesDefaults(t *testing.T) {
	setupContinueEnv(t)
	if err := RunContinue(t.Context(), ContinueOptions{
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
		row  tasks.Task
		want string
	}{
		{"verify-end-wins", tasks.Task{PlanEndAt: t1, WorkEndAt: t2, VerifyEndAt: t3}, "verify"},
		{"work-end-wins", tasks.Task{PlanEndAt: t1, WorkEndAt: t3}, "work"},
		{"plan-end-only", tasks.Task{PlanEndAt: t1}, "plan"},
		{"verify-cursor-fallback", tasks.Task{VerifyResumeSession: "v"}, "verify"},
		{"work-cursor-fallback", tasks.Task{WorkResumeSession: "w"}, "work"},
		{"plan-cursor-fallback", tasks.Task{PlanResumeSession: "p"}, "plan"},
		{"no-signal", tasks.Task{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := latestPhase(c.row); got != c.want {
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
	want := []string{"from-task", "interactive", "model", "tool"}
	if len(names) != len(want) {
		t.Fatalf("flags = %v, want %v", names, want)
	}
	for i, n := range names {
		if n != want[i] {
			t.Fatalf("flags[%d] = %q, want %q", i, n, want[i])
		}
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
	cmd.SetContext(t.Context())
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

// TestRunContinue_GetTaskDecodeError plants malformed JSON under an id
// so resolveContinueTaskFromStore -> GetTask returns a non-fs.ErrNotExist
// error.
func TestRunContinue_GetTaskDecodeError(t *testing.T) {
	setupContinueEnv(t)
	testutil.SeedRawTaskFile(t, "broken", []byte("not = valid = toml"))
	err := RunContinue(t.Context(), ContinueOptions{
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

func TestResolveContinueTaskFromStore_PickedMissingTask(t *testing.T) {
	setupContinueEnv(t)
	testutil.SeedFullTask(t, nil)
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	_, ok, err := resolveContinueTaskFromStore(t.Context(), s, ContinueOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
		UI:     &fakeUI{pickReturn: "missing"},
	})
	if err == nil || ok {
		t.Fatalf("resolve = ok %v, err %v; want missing task error", ok, err)
	}
}

// TestDispatchByStatus_UnknownStatus covers the safety-net branch.
func TestDispatchByStatus_UnknownStatus(t *testing.T) {
	err := dispatchByStatus(t.Context(), ContinueOptions{
		Stdout: io.Discard,
		Stderr: io.Discard,
	}, tasks.Task{ID: "x", Status: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "unsupported status") {
		t.Fatalf("err = %v", err)
	}
}

// TestRunContinue_PlanningSpawnFails verifies that planning status no
// longer attempts a spawn — it prints a tooltip and returns nil.
func TestRunContinue_PlanningSpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusPlanning
	})
	agent := newContinueAgent()
	var stdout bytes.Buffer
	err := RunContinue(t.Context(), ContinueOptions{
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
	want := "use `j tasks resume-plan`"
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want substring %q", stdout.String(), want)
	}
}

// TestNewContinueCmd_ToolModelFlagsBindToViper covers the --tool / --model
// flags viper bindings for `j tasks continue`.
func TestNewContinueCmd_ToolModelFlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newContinueCmd()
	if err := cmd.Flags().Set("tool", "cursor"); err != nil {
		t.Fatalf("Flags().Set tool: %v", err)
	}
	if err := cmd.Flags().Set("model", "sonnet-4"); err != nil {
		t.Fatalf("Flags().Set model: %v", err)
	}
	if got := viper.GetString("tasks.continue.tool"); got != "cursor" {
		t.Errorf("tasks.continue.tool = %q", got)
	}
	if got := viper.GetString("tasks.continue.model"); got != "sonnet-4" {
		t.Errorf("tasks.continue.model = %q", got)
	}
}

// TestNewContinueCmd_InteractiveFlagBindToViper covers the --interactive
// flag viper binding.
func TestNewContinueCmd_InteractiveFlagBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newContinueCmd()
	if err := cmd.Flags().Set("interactive", "false"); err != nil {
		t.Fatalf("Flags().Set interactive: %v", err)
	}
	if viper.GetBool("tasks.continue.interactive") {
		t.Errorf("tasks.continue.interactive should be false")
	}
}

func TestStampSpawnOnRow_KnownTask(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil)
	var stderr bytes.Buffer
	stampSpawnOnRow(&stderr, id, "/tmp/agent.log")
	if stderr.Len() > 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
	// Verify the row was updated.
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.AgentLogPath != "/tmp/agent.log" {
		t.Fatalf("AgentLogPath = %q, want /tmp/agent.log",
			row.AgentLogPath)
	}
}

func TestStampSpawnOnRow_UnknownID(t *testing.T) {
	setupContinueEnv(t)
	var stderr bytes.Buffer
	stampSpawnOnRow(&stderr, "ghost-id", "")
	if !strings.Contains(stderr.String(), "ghost-id") {
		t.Fatalf("stderr = %q, want ghost-id mention", stderr.String())
	}
}

func TestPickReVerifyFromStore_EmptyBucket(t *testing.T) {
	setupContinueEnv(t)
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	var stdout bytes.Buffer
	_, ok, err := pickReVerifyFromStore(
		t.Context(), s,
		ReVerifyOptions{Stdout: &stdout, UI: &fakeUI{}},
	)
	if err != nil || ok {
		t.Fatalf("pickReVerifyFromStore: ok=%v, err=%v, want (false, nil)",
			ok, err)
	}
	if !strings.Contains(stdout.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), emptyMessage)
	}
}

func TestPickReVerifyFromStore_PickerHappy(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, nil)
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ui := &fakeUI{pickReturn: id}
	got, ok, err := pickReVerifyFromStore(
		t.Context(), s,
		ReVerifyOptions{Stdout: io.Discard, UI: ui},
	)
	if err != nil || !ok {
		t.Fatalf("pickReVerifyFromStore = (%q, %v, %v)",
			got, ok, err)
	}
	if got != id {
		t.Fatalf("id = %q, want %q", got, id)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
}

func TestNewContinueCmd_RunE_InteractiveFlag(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setupContinueEnv(t)
	cmd := newContinueCmd()
	cmd.SetContext(t.Context())
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	if err := cmd.Flags().Set("interactive", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE with --interactive=true: %v", err)
	}
	if !strings.Contains(stdout.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q",
			stdout.String(), emptyMessage)
	}
}

// TestNewContinueCmd_ToolModelEnvBindings covers TASKS_CONTINUE_TOOL
// and TASKS_CONTINUE_MODEL env var bindings.
func TestNewContinueCmd_ToolModelEnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_CONTINUE_TOOL", "claude")
	t.Setenv("TASKS_CONTINUE_MODEL", "opus-4")
	_ = newContinueCmd()
	if got := viper.GetString("tasks.continue.tool"); got != "claude" {
		t.Errorf("tasks.continue.tool = %q", got)
	}
	if got := viper.GetString("tasks.continue.model"); got != "opus-4" {
		t.Errorf("tasks.continue.model = %q", got)
	}
}

// TestRunContinue_PlanApproveDispatchesToOrchestratorFromWork pins
// plan-pending-approval -> dispatchPlanApprove: fires EventPlanApprove,
// persists plan-done, then dispatches through the orchestrator with
// --phase=from-work (inheriting the runPlanDoneWork fix).
func TestRunContinue_PlanApproveDispatchesToOrchestratorFromWork(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusPlanPendingApproval
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call (spawned child runs the chain): planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{
		"tasks", "orchestrate",
		"--id", id,
		"--phase=from-work",
		"--interactive=false",
	}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
	row := readTaskFromBolt(t, id)
	if row.Status != tasks.StatusPlanDone {
		t.Fatalf("Status = %q, want plan-done (EventPlanApprove fired)", row.Status)
	}
}

// TestRunContinue_NeedsClarification_VerifyResume pins
// needs-clarification -> EventVerifyResume when VerifyEndAt is freshest:
// persists verifying and spawns inline orchestrator with
// --phase=verify-only --interactive=true.
func TestRunContinue_NeedsClarification_VerifyResume(t *testing.T) {
	setupContinueEnv(t)
	t1 := time.Now().UTC().Add(-3 * time.Hour)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusNeedsClarification
		task.VerifyResumeSession = "verify-cursor"
		task.PlanEndAt = t1
		task.WorkEndAt = t2
		task.VerifyEndAt = t3
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call: planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{
		"tasks", "orchestrate",
		"--id", id,
		"--phase=verify-only",
		"--interactive=true",
	}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
	row := readTaskFromBolt(t, id)
	if row.Status != tasks.StatusVerifying {
		t.Fatalf("Status = %q, want verifying (EventVerifyResume fired)", row.Status)
	}
}

// TestRunContinue_NeedsClarification_WorkResume pins
// needs-clarification -> EventWorkResume when WorkEndAt is freshest:
// persists working and spawns inline orchestrator with
// --phase=from-work --interactive=true.
func TestRunContinue_NeedsClarification_WorkResume(t *testing.T) {
	setupContinueEnv(t)
	t1 := time.Now().UTC().Add(-2 * time.Hour)
	t2 := t1.Add(time.Hour)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusNeedsClarification
		task.WorkResumeSession = "work-cursor"
		task.PlanEndAt = t1
		task.WorkEndAt = t2
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	agent := newContinueAgent()
	err := RunContinue(t.Context(), ContinueOptions{
		TaskID:  id,
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{agent},
		UI:      &fakeUI{},
		JBinary: argvJBinary(t, argvPath),
	})
	if err != nil {
		t.Fatalf("RunContinue: %v", err)
	}
	if agent.planned+agent.worked+agent.verified != 0 {
		t.Fatalf("no in-process agent call: planned=%d worked=%d verified=%d",
			agent.planned, agent.worked, agent.verified)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{
		"tasks", "orchestrate",
		"--id", id,
		"--phase=from-work",
		"--interactive=true",
	}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
	row := readTaskFromBolt(t, id)
	if row.Status != tasks.StatusWorking {
		t.Fatalf("Status = %q, want working (EventWorkResume fired)", row.Status)
	}
}
