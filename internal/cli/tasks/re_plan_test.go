package tasks

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

// TestRunRePlan_NoTasks pins the empty-store branch: no rows, no
// --from-task -> emptyMessage on stdout and exit 0.
func TestRunRePlan_NoTasks(t *testing.T) {
	setupContinueEnv(t)
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunRePlan(t.Context(), RePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	if !strings.Contains(stdout.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), emptyMessage)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask calls = %d, want 0 on empty store", ui.pickCalls)
	}
}

// TestRunRePlan_PickerAbort pins the cancel signal: PickTask returns
// ok=false; RunRePlan exits cleanly with no spawn.
func TestRunRePlan_PickerAbort(t *testing.T) {
	setupContinueEnv(t)
	seedTaskFull(t, nil)
	ui := &fakeUI{} // empty pickReturn -> ok=false
	if err := RunRePlan(t.Context(), RePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if ui.statusCalls != 0 {
		t.Fatalf("ConfirmStatusOverride should not fire on picker abort: calls=%d", ui.statusCalls)
	}
}

// TestRunRePlan_FromTaskNotFound pins the explicit-id branch: an
// unknown id surfaces the wrapped resolver error verbatim.
func TestRunRePlan_FromTaskNotFound(t *testing.T) {
	setupContinueEnv(t)
	err := RunRePlan(t.Context(), RePlanOptions{
		FromTask: "ghost",
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newContinueAgent()},
		UI:       &fakeUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want not-found wrap", err)
	}
}

// TestRunRePlan_StatusOverrideDeclined pins the confirm-no branch: a
// task in `working` (outside the re-plan allowlist) renders the
// status-override prompt; declining it short-circuits with no spawn.
func TestRunRePlan_StatusOverrideDeclined(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorking
	})
	ui := &fakeUI{statusReturn: false}
	if err := RunRePlan(t.Context(), RePlanOptions{
		FromTask: id,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newContinueAgent()},
		UI:       ui,
		JBinary:  "/should/not/be/spawned",
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	if ui.statusCalls != 1 {
		t.Fatalf("ConfirmStatusOverride calls = %d, want 1", ui.statusCalls)
	}
	if ui.statusTaskID != id || ui.statusCmd != "re-plan" || ui.statusStatus != string(tasks.StatusWorking) {
		t.Fatalf("ConfirmStatusOverride args = (%q, %q, %q), want (re-plan, %q, %q)",
			ui.statusCmd, ui.statusTaskID, ui.statusStatus, id, tasks.StatusWorking)
	}
	row := readTaskFromBolt(t, id)
	if row.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (decline must not fire spawn)", row.BackgroundPID)
	}
}

// TestRunRePlan_PlanDoneSkipsConfirm pins the allowlist branch:
// plan-done is a "natural" re-plan target and must skip the prompt.
func TestRunRePlan_PlanDoneSkipsConfirm(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil) // default Status: plan-done
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{}
	var stdout bytes.Buffer
	if err := RunRePlan(t.Context(), RePlanOptions{
		FromTask:    id,
		Interactive: new(false),
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          ui,
		JBinary:     argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	if ui.statusCalls != 0 {
		t.Fatalf("ConfirmStatusOverride should be skipped for plan-done: calls=%d", ui.statusCalls)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{"tasks", "orchestrate", "--id", id, "--plan-requires-approval=true", "--interactive=false"}
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

// TestRunRePlan_HelpSkipsConfirm pins the second allowlist branch:
// `help` is also a natural re-plan target and must skip the prompt.
func TestRunRePlan_HelpSkipsConfirm(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusHelp
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{}
	if err := RunRePlan(t.Context(), RePlanOptions{
		FromTask: id,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newContinueAgent()},
		UI:       ui,
		JBinary:  argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	if ui.statusCalls != 0 {
		t.Fatalf("ConfirmStatusOverride should be skipped for help: calls=%d", ui.statusCalls)
	}
	args := readSpawnedArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate ...`", args)
	}
}

// TestRunRePlan_ForwardsAllOverrides pins that --tool / --model /
// --interactive flow into the orchestrate argv as one-off overrides.
func TestRunRePlan_ForwardsAllOverrides(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	if err := RunRePlan(t.Context(), RePlanOptions{
		FromTask:    id,
		Tool:        "claude",
		Model:       "opus",
		Interactive: new(true),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          &fakeUI{},
		JBinary:     argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate",
		"--id", id,
		"--plan-requires-approval=true",
		"--interactive=true",
		"--tool=claude",
		"--model=opus",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
}

// TestRunRePlan_InteractiveFalseStillForwards pins that
// Interactive=false (vs. nil/inherit) is forwarded explicitly.
func TestRunRePlan_InteractiveFalseStillForwards(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	if err := RunRePlan(t.Context(), RePlanOptions{
		FromTask:    id,
		Interactive: new(false),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          &fakeUI{},
		JBinary:     argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	if got := args[len(args)-1]; got != "--interactive=false" {
		t.Fatalf("last arg = %q, want --interactive=false; argv=%v", got, args)
	}
}

// TestRunRePlan_PickerHappy drives the no-flag picker path: the user
// picks a row and the spawn fires for it.
func TestRunRePlan_PickerHappy(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	if err := RunRePlan(t.Context(), RePlanOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	args := readSpawnedArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate ...`", args)
	}
}

// TestRunRePlan_InteractiveRunsInline pins the foreground path:
// --interactive=true re-execs `j tasks orchestrate` inline (blocking,
// terminal-attached). No fork dialog fires and the row's
// BackgroundPID stays 0 because the inline exec owns its own PID.
func TestRunRePlan_InteractiveRunsInline(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	var stdout bytes.Buffer
	if err := RunRePlan(t.Context(), RePlanOptions{
		FromTask:    id,
		Interactive: new(true),
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          &fakeUI{},
		JBinary:     argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{"tasks", "orchestrate", "--id", id, "--plan-requires-approval=true", "--interactive=true"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
	if strings.Contains(stdout.String(), "running in background") || strings.Contains(stdout.String(), "tail -f") {
		t.Fatalf("stdout = %q, want no fork dialog (inline exec)", stdout.String())
	}
	row := readTaskFromBolt(t, id)
	if row.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (inline exec)", row.BackgroundPID)
	}
}

// TestRunRePlan_SpawnFails pins the SpawnIn error branch: pointing
// JBinary at a missing path surfaces the spawn error.
func TestRunRePlan_SpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	err := RunRePlan(t.Context(), RePlanOptions{
		FromTask: id,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newContinueAgent()},
		UI:       &fakeUI{},
		JBinary:  "/no/such/binary-xyzzy",
	})
	if err == nil {
		t.Fatal("expected spawn failure")
	}
	row := readTaskFromBolt(t, id)
	if row.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (no row mutation on spawn failure)", row.BackgroundPID)
	}
}

// TestRunRePlan_AppliesDefaults exercises RePlanOptions.withDefaults
// (the nil-stream / nil-UI branches) by running with an explicit
// FromTask and pre-populated agent buckets so the UI is never invoked.
func TestRunRePlan_AppliesDefaults(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	if err := RunRePlan(t.Context(), RePlanOptions{
		FromTask: id,
		Agents:   []codingagents.Agent{newContinueAgent()},
		JBinary:  noopJBinary(t),
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
}

// TestNewRePlanCmd_FlagDefaults pins the registered flag set.
func TestNewRePlanCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRePlanCmd()
	if cmd.Use != "re-plan" {
		t.Fatalf("Use = %q, want re-plan", cmd.Use)
	}
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	want := []string{"from-task", "interactive", "model", "tool"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("flags = %v, want %v", names, want)
	}
}

// TestNewRePlanCmd_FlagsBindToViper covers flag→viper bindings.
func TestNewRePlanCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRePlanCmd()
	for flag, key := range map[string]string{
		"from-task": "tasks.replan.from_task",
		"tool":      "tasks.replan.tool",
		"model":     "tasks.replan.model",
	} {
		if err := cmd.Flags().Set(flag, "testval"); err != nil {
			t.Fatalf("Flags().Set %s: %v", flag, err)
		}
		if got := viper.GetString(key); got != "testval" {
			t.Errorf("%s = %q, want testval", key, got)
		}
	}
	if err := cmd.Flags().Set("interactive", "true"); err != nil {
		t.Fatalf("Flags().Set interactive: %v", err)
	}
	if !viper.GetBool("tasks.replan.interactive") {
		t.Error("tasks.replan.interactive = false, want true")
	}
}

// TestNewRePlanCmd_EnvBindings covers env-var→viper bindings.
func TestNewRePlanCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_REPLAN_FROM_TASK", "task-id")
	t.Setenv("TASKS_REPLAN_TOOL", "claude")
	t.Setenv("TASKS_REPLAN_MODEL", "opus")
	t.Setenv("TASKS_REPLAN_INTERACTIVE", "true")
	_ = newRePlanCmd()
	if got := viper.GetString("tasks.replan.from_task"); got != "task-id" {
		t.Errorf("tasks.replan.from_task = %q", got)
	}
	if got := viper.GetString("tasks.replan.tool"); got != "claude" {
		t.Errorf("tasks.replan.tool = %q", got)
	}
	if got := viper.GetString("tasks.replan.model"); got != "opus" {
		t.Errorf("tasks.replan.model = %q", got)
	}
	if !viper.GetBool("tasks.replan.interactive") {
		t.Error("tasks.replan.interactive = false, want true")
	}
}

// TestNewRePlanCmd_RunE_PropagatesError exercises the RunE closure
// end to end with --from-task pointing at a missing id.
func TestNewRePlanCmd_RunE_PropagatesError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setupContinueEnv(t)
	cmd := newRePlanCmd()
	if err := cmd.Flags().Set("from-task", "ghost"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("expected an error from missing --from-task id")
	}
}

// TestRunRePlan_StatusUIError pins the explicit-error branch from
// ConfirmStatusOverride: a non-aborted UI error must propagate to
// the caller and leave the row untouched.
func TestRunRePlan_StatusUIError(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorking
	})
	boom := errInjected("status boom")
	ui := &fakeUI{statusErr: boom}
	err := RunRePlan(t.Context(), RePlanOptions{
		FromTask: id,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newContinueAgent()},
		UI:       ui,
	})
	if err == nil || !strings.Contains(err.Error(), "status boom") {
		t.Fatalf("err = %v, want status boom propagation", err)
	}
}

// TestRunRePlan_OpenDefaultFails replaces the cwd with a removed
// directory so tasks.OpenDefault → DefaultDir → os.Getwd fails. On
// macOS getwd may still succeed via cached inodes; in that case the
// test skips. Drives the resolveRePlanTaskID error branch.
func TestRunRePlan_OpenDefaultFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cwd cannot be removed while in use on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root may bypass relevant FS errors")
	}
	parent := t.TempDir()
	gone := filepath.Join(parent, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	t.Setenv("PWD", "")
	if err := os.Remove(gone); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd unexpectedly succeeded")
	}
	err := RunRePlan(t.Context(), RePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     &fakeUI{},
	})
	if err == nil {
		t.Fatal("expected DefaultDir to surface getwd error")
	}
}

// TestRunRePlan_ListDecodeError plants malformed TOML so ListTasks
// surfaces a decode error after the picker branch opens the store.
func TestRunRePlan_ListDecodeError(t *testing.T) {
	setupContinueEnv(t)
	if _, err := tasks.EnsureDir("bad"); err != nil {
		t.Fatal(err)
	}
	dir, err := tasks.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad", tasks.TaskFileName), []byte("not = valid = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = RunRePlan(t.Context(), RePlanOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     &fakeUI{},
	})
	if err == nil || !strings.Contains(err.Error(), "decode task") {
		t.Fatalf("err = %v, want decode-task error", err)
	}
}

// TestNewRePlanCmd_PreRunE_DefaultedAgents pins that the registered
// PreRunE delegates to preflight.EnsureAgentSelections with the
// wired cursor+claude pair. The seeded buckets satisfy the check
// without prompting, exercising the closure end to end.
func TestNewRePlanCmd_PreRunE_DefaultedAgents(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setupContinueEnv(t)
	installCursorAgentLoginStub(t)
	cmd := newRePlanCmd()
	cmd.SetContext(t.Context())
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
}

// TestNewRePlanCmd_RunE_InteractiveFlag drives the closure with
// --interactive=true and --from-task pointing at a missing id so the
// closure exercises the env-or-flag branch (flipping the *bool
// override on) and exits cleanly with a "task not found" error
// before the spawn fires.
func TestNewRePlanCmd_RunE_InteractiveFlag(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setupContinueEnv(t)
	cmd := newRePlanCmd()
	if err := cmd.Flags().Set("from-task", "ghost"); err != nil {
		t.Fatalf("Flags().Set from-task: %v", err)
	}
	if err := cmd.Flags().Set("interactive", "true"); err != nil {
		t.Fatalf("Flags().Set interactive: %v", err)
	}
	cmd.SetContext(t.Context())
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("expected `task ghost not found` error")
	}
}

// TestRunRePlan_FromCompletedSpawnsAfterConfirm pins that re-plan no
// longer rejects a completed task at the IsLegal guard. The status
// is outside the re-plan allowlist so the override prompt fires;
// confirming reaches the orchestrator-spawn fake.
func TestRunRePlan_FromCompletedSpawnsAfterConfirm(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{statusReturn: true}
	if err := RunRePlan(t.Context(), RePlanOptions{
		FromTask:    id,
		Interactive: new(false),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          ui,
		JBinary:     argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	if ui.statusCalls != 1 {
		t.Fatalf("ConfirmStatusOverride calls = %d, want 1",
			ui.statusCalls)
	}
	args := readSpawnedArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate ...`", args)
	}
}

// TestRunRePlan_FromFailedSpawnsAfterConfirm mirrors the completed
// case for the `failed` source status.
func TestRunRePlan_FromFailedSpawnsAfterConfirm(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{statusReturn: true}
	if err := RunRePlan(t.Context(), RePlanOptions{
		FromTask:    id,
		Interactive: new(false),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          ui,
		JBinary:     argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunRePlan: %v", err)
	}
	if ui.statusCalls != 1 {
		t.Fatalf("ConfirmStatusOverride calls = %d, want 1",
			ui.statusCalls)
	}
	args := readSpawnedArgv(t, argvPath)
	if len(args) == 0 || args[0] != "tasks" {
		t.Fatalf("argv = %v, want spawned `tasks orchestrate ...`", args)
	}
}

// TestRunRePlan_RegisteredAsChild pins the cobra wiring.
func TestRunRePlan_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "re-plan" {
			return
		}
	}
	t.Fatal("`j tasks re-plan` should be registered as a child of `j tasks`")
}
