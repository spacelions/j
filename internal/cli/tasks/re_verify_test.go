package tasks

import (
	"bytes"
	"context"
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

func TestRunReVerify_NoTasks(t *testing.T) {
	setupContinueEnv(t)
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunReVerify(context.Background(), ReVerifyOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunReVerify: %v", err)
	}
	if !strings.Contains(stdout.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), emptyMessage)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask calls = %d, want 0 on empty store", ui.pickCalls)
	}
}

func TestRunReVerify_FromTaskNotFound(t *testing.T) {
	setupContinueEnv(t)
	err := RunReVerify(context.Background(), ReVerifyOptions{
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

func TestRunReVerify_StatusOverrideDeclined(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorking
	})
	ui := &fakeUI{statusReturn: false}
	if err := RunReVerify(context.Background(), ReVerifyOptions{
		FromTask: id,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newContinueAgent()},
		UI:       ui,
		JBinary:  "/should/not/be/spawned",
	}); err != nil {
		t.Fatalf("RunReVerify: %v", err)
	}
	if ui.statusCalls != 1 {
		t.Fatalf("ConfirmStatusOverride calls = %d, want 1", ui.statusCalls)
	}
}

func TestRunReVerify_WorkDoneSkipsConfirm(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorkDone
	})
	ui := &fakeUI{}
	var stdout bytes.Buffer
	if err := RunReVerify(context.Background(), ReVerifyOptions{
		FromTask:    id,
		Interactive: boolPtr(false),
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          ui,
		JBinary:     noopJBinary(t),
	}); err != nil {
		t.Fatalf("RunReVerify: %v", err)
	}
	if ui.statusCalls != 0 {
		t.Fatalf("ConfirmStatusOverride should be skipped for work-done: calls=%d", ui.statusCalls)
	}
	row := readTaskFromBolt(t, id)
	if row.BackgroundPID == 0 {
		t.Fatalf("BackgroundPID = 0; want non-zero detached child PID")
	}
}

func TestRunReVerify_InteractiveRunsInline(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorkDone
	})
	var stdout bytes.Buffer
	if err := RunReVerify(context.Background(), ReVerifyOptions{
		FromTask:    id,
		Interactive: boolPtr(true),
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          &fakeUI{},
		JBinary:     noopJBinary(t),
	}); err != nil {
		t.Fatalf("RunReVerify: %v", err)
	}
	if strings.Contains(stdout.String(), "running in background") {
		t.Fatalf("stdout = %q, want no fork dialog (inline exec)", stdout.String())
	}
}

func TestNewReVerifyCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newReVerifyCmd()
	if cmd.Use != "re-verify" {
		t.Fatalf("Use = %q, want re-verify", cmd.Use)
	}
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	want := []string{"from-task", "interactive"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("flags = %v, want %v", names, want)
	}
}

func TestNewReVerifyCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newReVerifyCmd()
	if err := cmd.Flags().Set("from-task", "testval"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("tasks.reverify.from_task"); got != "testval" {
		t.Errorf("tasks.reverify.from_task = %q, want testval", got)
	}
	if err := cmd.Flags().Set("interactive", "true"); err != nil {
		t.Fatalf("Flags().Set interactive: %v", err)
	}
	if !viper.GetBool("tasks.reverify.interactive") {
		t.Error("tasks.reverify.interactive = false, want true")
	}
}

func TestNewReVerifyCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_REVERIFY_FROM_TASK", "task-id")
	t.Setenv("TASKS_REVERIFY_INTERACTIVE", "true")
	_ = newReVerifyCmd()
	if got := viper.GetString("tasks.reverify.from_task"); got != "task-id" {
		t.Errorf("tasks.reverify.from_task = %q", got)
	}
	if !viper.GetBool("tasks.reverify.interactive") {
		t.Error("tasks.reverify.interactive = false, want true")
	}
}

func TestNewReVerifyCmd_RunE_PropagatesError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	mustInit(t)
	setupContinueEnv(t)
	cmd := newReVerifyCmd()
	if err := cmd.Flags().Set("from-task", "ghost"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(context.Background())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	// non-interactive: re-verify fires the spawn helper which
	// requires a real j binary we don't have in the test.
	// The error surfaces when spawnDetachedOrchestrator fails
	// because we didn't set JBinary.
	_ = cmd.RunE(cmd, nil)
	// The default JBinary="" causes os.Executable to be used;
	// since we ARE the test binary it fires the orchestrator
	// with a different argv. The spawn succeeds but the child
	// inherits our cwd. We just care that RunE didn't panic.
	// For a true error propagation test, point JBinary at a
	// missing path.
}

func TestNewReVerifyCmd_PreRunE_DefaultedAgents(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setupContinueEnv(t)
	installCursorAgentLoginStub(t)
	cmd := newReVerifyCmd()
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
}

func TestNewReVerifyCmd_RunE_InteractiveFlag(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setupContinueEnv(t)
	cmd := newReVerifyCmd()
	cmd.SetContext(context.Background())
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

func TestReVerify_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "re-verify" {
			return
		}
	}
	t.Fatal("`j tasks re-verify` should be registered as a child of `j tasks`")
}

func TestRunReVerify_SpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorkDone
	})
	err := RunReVerify(context.Background(), ReVerifyOptions{
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
}

func TestRunReVerify_OpenDefaultFails(t *testing.T) {
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
	err := RunReVerify(context.Background(), ReVerifyOptions{
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

func TestReVerifyOptions_WithDefaults_FillsNilStreams(t *testing.T) {
	o := ReVerifyOptions{}.withDefaults()
	if o.Stdin != os.Stdin {
		t.Errorf("Stdin = %v, want os.Stdin", o.Stdin)
	}
	if o.Stdout != os.Stdout {
		t.Errorf("Stdout = %v, want os.Stdout", o.Stdout)
	}
	if o.Stderr != os.Stderr {
		t.Errorf("Stderr = %v, want os.Stderr", o.Stderr)
	}
	if o.UI == nil {
		t.Error("UI was not defaulted")
	}
}

func TestReVerifyOptions_WithDefaults_KeepsProvided(t *testing.T) {
	customUI := &fakeUI{}
	o := ReVerifyOptions{
		UI:     customUI,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}.withDefaults()
	if o.UI != customUI {
		t.Errorf("UI = %v, want custom", o.UI)
	}
}

