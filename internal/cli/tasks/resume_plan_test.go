package tasks

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

// TestRunResumePlan_NoActiveSession pins the empty-filtered-list
// branch: every row has PlanResumeSession == "" so the picker never
// fires and the user-facing message is printed instead.
func TestRunResumePlan_NoActiveSession(t *testing.T) {
	setupContinueEnv(t)
	testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.PlanResumeSession = ""
	})
	ui := &fakeUI{}
	var stdout bytes.Buffer
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	if !strings.Contains(stdout.String(), noActivePlanSessionMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), noActivePlanSessionMessage)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask should not fire when no session-bearing tasks exist: calls=%d", ui.pickCalls)
	}
}

// TestRunResumePlan_NoTasksAtAll pins the totally-empty store branch:
// no rows at all -> no-active-session message and exit 0.
func TestRunResumePlan_NoTasksAtAll(t *testing.T) {
	setupContinueEnv(t)
	ui := &fakeUI{}
	var stdout bytes.Buffer
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	if !strings.Contains(stdout.String(), noActivePlanSessionMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), noActivePlanSessionMessage)
	}
}

// TestRunResumePlan_PickerOnlyShowsRowsWithSession pins the filter:
// rows without PlanResumeSession are not surfaced to the picker.
func TestRunResumePlan_PickerOnlyShowsRowsWithSession(t *testing.T) {
	setupContinueEnv(t)
	keep := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	skip := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.PlanResumeSession = ""
	})
	ui := &fakeUI{} // empty pickReturn -> picker abort
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if len(ui.lastPickedFrom) != 1 {
		t.Fatalf("picker received %d rows, want 1", len(ui.lastPickedFrom))
	}
	if ui.lastPickedFrom[0].ID != keep {
		t.Fatalf("picker received id %q, want %q (the row with PlanResumeSession set; %q should have been filtered out)",
			ui.lastPickedFrom[0].ID, keep, skip)
	}
}

// TestRunResumePlan_PickerAbort pins the cancel signal: PickTask
// returns ok=false; RunResumePlan exits cleanly with no spawn.
func TestRunResumePlan_PickerAbort(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	ui := &fakeUI{}
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	row := readTaskFromBolt(t, id)
	if row.AgentLogPath != "" {
		t.Fatalf("AgentLogPath = %q, want empty (picker abort must not fire spawn)", row.AgentLogPath)
	}
}

// TestRunResumePlan_HappyPath pins the inline-exec path: a row with
// PlanResumeSession set, a stub J binary recording its argv. The
// argv must be `tasks orchestrate --id <id> --plan-requires-approval=true
// --interactive=true` (resume-plan always runs inline with a TUI),
// and no fork dialog fires.
func TestRunResumePlan_HappyPath(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	var stdout bytes.Buffer
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{"tasks", "orchestrate", "--id", id, "--plan-requires-approval=true", "--interactive=true"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
	_ = readTaskFromBolt(t, id)
	if strings.Contains(stdout.String(), "running in background") || strings.Contains(stdout.String(), "tail -f") {
		t.Fatalf("stdout = %q, want no fork dialog (inline exec)", stdout.String())
	}
}

// TestRunResumePlan_HappyPath_PlanPendingApproval pins that resume-plan
// succeeds for a row already at the approval gate. The FSM edge
// {plan-pending-approval, EventPlanResume, planning} must be present
// for resume_plan.go's IsLegal guard to permit the transition; this
// test fails if the edge is removed.
func TestRunResumePlan_HappyPath_PlanPendingApproval(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusPlanPendingApproval
		task.PlanResumeSession = "active-cursor"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--plan-requires-approval=true", "--interactive=true",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
}

// TestRunResumePlan_SpawnFails pins the inline-exec error branch:
// pointing JBinary at a missing path surfaces the run.RunIn error.
func TestRunResumePlan_SpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	ui := &fakeUI{pickReturn: id}
	err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: "/no/such/binary-xyzzy",
	})
	if err == nil {
		t.Fatal("expected exec failure")
	}
}

// TestRunResumePlan_PickerErrorPropagates pins the explicit-error
// branch from the picker (something other than abort).
func TestRunResumePlan_PickerErrorPropagates(t *testing.T) {
	setupContinueEnv(t)
	testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	boom := errInjected("picker boom")
	ui := &fakeUI{pickErr: boom}
	err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	})
	if err == nil || !strings.Contains(err.Error(), "picker boom") {
		t.Fatalf("err = %v, want picker boom propagation", err)
	}
}

// TestRunResumePlan_ListDecodeError plants malformed TOML so
// ListTasks fails after the store opens. The error must propagate
// without invoking the picker.
func TestRunResumePlan_ListDecodeError(t *testing.T) {
	setupContinueEnv(t)
	// Plant a malformed task.toml so ListTasks surfaces a decode error.
	if _, err := tasks.EnsureDir("bad"); err != nil {
		t.Fatal(err)
	}
	dir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad", tasks.TaskFileName)
	if err := os.WriteFile(bad, []byte("not = valid = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	ui := &fakeUI{}
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err == nil {
		t.Fatal("expected ListTasks decode error to propagate")
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask should not fire on list decode error: calls=%d", ui.pickCalls)
	}
}

// TestRunResumePlan_AppliesDefaults exercises ResumePlanOptions.withDefaults
// (the nil-stream / nil-UI branches). The empty store short-circuits
// before any UI invocation, so the test only confirms the helper does
// not panic and returns nil.
func TestRunResumePlan_AppliesDefaults(t *testing.T) {
	setupContinueEnv(t)
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Agents: []codingagents.Agent{newContinueAgent()},
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
}

// TestNewResumePlanCmd_FlagDefaults pins the registered flag set
// (none — picker-only interface).
func TestNewResumePlanCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newResumePlanCmd()
	if cmd.Use != "resume-plan" {
		t.Fatalf("Use = %q, want resume-plan", cmd.Use)
	}
	if cmd.Flags().HasFlags() {
		t.Fatal("resume-plan should not register any flags")
	}
}

// TestNewResumePlanCmd_RunE_EmptyStore drives the closure end to
// end with no list.db on disk; the no-active-session message is
// printed and RunE returns nil.
func TestNewResumePlanCmd_RunE_EmptyStore(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	cmd := newResumePlanCmd()
	cmd.SetContext(t.Context())
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(stdout.String(), noActivePlanSessionMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), noActivePlanSessionMessage)
	}
}

// TestNewResumePlanCmd_PreRunE_DefaultedAgents pins that the
// registered PreRunE delegates to preflight.EnsureAgentSelections
// with the wired cursor+claude pair. The seeded buckets satisfy the
// check without prompting, exercising the closure end to end.
func TestNewResumePlanCmd_PreRunE_DefaultedAgents(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setupContinueEnv(t)
	installCursorAgentLoginStub(t)
	cmd := newResumePlanCmd()
	cmd.SetContext(t.Context())
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
}

// TestRunResumePlan_HappyPath_Completed pins that resume-plan
// succeeds for a completed row carrying a plan resume session. The
// FSM edge {completed, EventPlanResume, planning} must permit it.
func TestRunResumePlan_HappyPath_Completed(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
		task.PlanResumeSession = "sess-x"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--plan-requires-approval=true", "--interactive=true",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
}

// TestRunResumePlan_HappyPath_Failed mirrors the completed case for
// the `failed` source status.
func TestRunResumePlan_HappyPath_Failed(t *testing.T) {
	setupContinueEnv(t)
	id := testutil.SeedFullTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
		task.PlanResumeSession = "sess-x"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	if err := RunResumePlan(t.Context(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--plan-requires-approval=true", "--interactive=true",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
}

// TestRunResumePlan_RegisteredAsChild pins the cobra wiring.
func TestRunResumePlan_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "resume-plan" {
			return
		}
	}
	t.Fatal("`j tasks resume-plan` should be registered as a child of `j tasks`")
}

// TestFilterTasksWithPlanSession pins the predicate body in isolation.
func TestFilterTasksWithPlanSession(t *testing.T) {
	rows := []tasks.Task{
		{ID: "a", PlanResumeSession: "x"},
		{ID: "b", PlanResumeSession: ""},
		{ID: "c", PlanResumeSession: "y"},
	}
	got := filterTasksBySession(rows, resumePlanConfig.hasSession)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("filtered = %+v, want [a c]", got)
	}
}

// errInjected is a minimal error type for picker-error tests.
type errInjected string

func (e errInjected) Error() string { return string(e) }
