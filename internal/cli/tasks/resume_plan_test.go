package tasks

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestRunResumePlan_NoActiveSession pins the empty-filtered-list
// branch: every row has PlanResumeSession == "" so the picker never
// fires and the user-facing message is printed instead.
func TestRunResumePlan_NoActiveSession(t *testing.T) {
	setupContinueEnv(t)
	seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = ""
	})
	ui := &fakeUI{}
	var stdout bytes.Buffer
	if err := RunResumePlan(context.Background(), ResumePlanOptions{
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
	if err := RunResumePlan(context.Background(), ResumePlanOptions{
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
	keep := seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	skip := seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = ""
	})
	ui := &fakeUI{} // empty pickReturn -> picker abort
	if err := RunResumePlan(context.Background(), ResumePlanOptions{
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
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	ui := &fakeUI{}
	if err := RunResumePlan(context.Background(), ResumePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumePlan: %v", err)
	}
	row := readTaskFromBolt(t, id)
	if row.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (picker abort must not fire spawn)", row.BackgroundPID)
	}
}

// TestRunResumePlan_HappyPath pins the spawn path: a row with
// PlanResumeSession set, a stub J binary recording its argv. The
// argv must be `tasks orchestrate --id <id> --plan-requires-approval=true`
// and the row must carry the spawned PID + AgentLogPath.
func TestRunResumePlan_HappyPath(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	var stdout bytes.Buffer
	if err := RunResumePlan(context.Background(), ResumePlanOptions{
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
	want := []string{"tasks", "orchestrate", "--id", id, "--plan-requires-approval=true"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
	row := readTaskFromBolt(t, id)
	if row.BackgroundPID == 0 {
		t.Fatalf("BackgroundPID = 0, want non-zero detached child PID")
	}
	wantLog := filepath.Join(".j/tasks", id, tasks.AgentLogFileName)
	if !strings.HasSuffix(row.AgentLogPath, wantLog) {
		t.Fatalf("AgentLogPath = %q, want suffix %q", row.AgentLogPath, wantLog)
	}
	if !strings.Contains(stdout.String(), "task "+id+" running in background") || !strings.Contains(stdout.String(), "tail -f") {
		t.Fatalf("stdout = %q, want fork dialog", stdout.String())
	}
}

// TestRunResumePlan_SpawnFails pins the SpawnIn error branch: pointing
// JBinary at a missing path surfaces the spawn error.
func TestRunResumePlan_SpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	ui := &fakeUI{pickReturn: id}
	err := RunResumePlan(context.Background(), ResumePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
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

// TestRunResumePlan_PickerErrorPropagates pins the explicit-error
// branch from the picker (something other than abort).
func TestRunResumePlan_PickerErrorPropagates(t *testing.T) {
	setupContinueEnv(t)
	seedTaskFull(t, func(task *tasks.Task) {
		task.PlanResumeSession = "active-cursor"
	})
	boom := errInjected("picker boom")
	ui := &fakeUI{pickErr: boom}
	err := RunResumePlan(context.Background(), ResumePlanOptions{
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
	if err := RunResumePlan(context.Background(), ResumePlanOptions{
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
	if err := RunResumePlan(context.Background(), ResumePlanOptions{
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
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	if len(names) != 0 {
		t.Fatalf("flags = %v, want none", names)
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
	cmd.SetContext(context.Background())
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
	cmd := newResumePlanCmd()
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
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
	got := filterTasksWithPlanSession(rows)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("filtered = %+v, want [a c]", got)
	}
}

// errInjected is a minimal error type for picker-error tests.
type errInjected string

func (e errInjected) Error() string { return string(e) }
