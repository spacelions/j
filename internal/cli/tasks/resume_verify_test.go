package tasks

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

func TestRunResumeVerify_NoActiveSession(t *testing.T) {
	setupContinueEnv(t)
	seedTaskFull(t, func(task *tasks.Task) {
		task.VerifyResumeSession = ""
	})
	ui := &fakeUI{}
	var stdout bytes.Buffer
	if err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumeVerify: %v", err)
	}
	if !strings.Contains(stdout.String(), noActiveVerifySessionMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), noActiveVerifySessionMessage)
	}
	if ui.pickCalls != 0 {
		t.Fatalf("PickTask should not fire: calls=%d", ui.pickCalls)
	}
}

func TestRunResumeVerify_NoTasksAtAll(t *testing.T) {
	setupContinueEnv(t)
	ui := &fakeUI{}
	var stdout bytes.Buffer
	if err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumeVerify: %v", err)
	}
	if !strings.Contains(stdout.String(), noActiveVerifySessionMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), noActiveVerifySessionMessage)
	}
}

func TestRunResumeVerify_PickerOnlyShowsRowsWithSession(t *testing.T) {
	setupContinueEnv(t)
	keep := seedTaskFull(t, func(task *tasks.Task) {
		task.VerifyResumeSession = "active-cursor"
	})
	skip := seedTaskFull(t, func(task *tasks.Task) {
		task.VerifyResumeSession = ""
	})
	ui := &fakeUI{}
	if err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumeVerify: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
	if len(ui.lastPickedFrom) != 1 {
		t.Fatalf("picker received %d rows, want 1", len(ui.lastPickedFrom))
	}
	if ui.lastPickedFrom[0].ID != keep {
		t.Fatalf("picker received id %q, want %q (the row with VerifyResumeSession set; %q should have been filtered out)",
			ui.lastPickedFrom[0].ID, keep, skip)
	}
}

func TestRunResumeVerify_PickerAbort(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.VerifyResumeSession = "active-cursor"
	})
	ui := &fakeUI{}
	if err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumeVerify: %v", err)
	}
	row := readTaskFromBolt(t, id)
	if row.BackgroundPID != 0 {
		t.Fatalf("BackgroundPID = %d, want 0 (picker abort must not fire spawn)", row.BackgroundPID)
	}
}

func TestRunResumeVerify_HappyPath(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.VerifyResumeSession = "active-cursor"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	var stdout bytes.Buffer
	if err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumeVerify: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{"tasks", "orchestrate", "--id", id, "--phase=verify-only", "--interactive=true"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
	if strings.Contains(stdout.String(), "running in background") {
		t.Fatalf("stdout = %q, want no fork dialog (inline exec)", stdout.String())
	}
}

func TestRunResumeVerify_SpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.VerifyResumeSession = "active-cursor"
	})
	ui := &fakeUI{pickReturn: id}
	err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
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

func TestRunResumeVerify_PickerErrorPropagates(t *testing.T) {
	setupContinueEnv(t)
	seedTaskFull(t, func(task *tasks.Task) {
		task.VerifyResumeSession = "active-cursor"
	})
	boom := errInjected("picker boom")
	ui := &fakeUI{pickErr: boom}
	err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
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

func TestRunResumeVerify_ListDecodeError(t *testing.T) {
	setupContinueEnv(t)
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
	if err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
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

func TestRunResumeVerify_AppliesDefaults(t *testing.T) {
	setupContinueEnv(t)
	if err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
		Agents: []codingagents.Agent{newContinueAgent()},
	}); err != nil {
		t.Fatalf("RunResumeVerify: %v", err)
	}
}

func TestNewResumeVerifyCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newResumeVerifyCmd()
	if cmd.Use != "resume-verify" {
		t.Fatalf("Use = %q, want resume-verify", cmd.Use)
	}
	if cmd.Flags().HasFlags() {
		t.Fatal("resume-verify should not register any flags")
	}
}

func TestNewResumeVerifyCmd_RunE_EmptyStore(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	mustInit(t)
	cmd := newResumeVerifyCmd()
	cmd.SetContext(context.Background())
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(stdout.String(), noActiveVerifySessionMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), noActiveVerifySessionMessage)
	}
}

func TestNewResumeVerifyCmd_PreRunE_DefaultedAgents(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setupContinueEnv(t)
	installCursorAgentLoginStub(t)
	cmd := newResumeVerifyCmd()
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE: %v", err)
	}
}

// TestRunResumeVerify_HappyPath_Completed pins that resume-verify
// succeeds for a completed row carrying a verify resume session. The
// FSM edge {completed, EventVerifyResume, verifying} must permit it.
func TestRunResumeVerify_HappyPath_Completed(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
		task.VerifyResumeSession = "sess-x"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	if err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumeVerify: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--phase=verify-only", "--interactive=true",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
}

// TestRunResumeVerify_HappyPath_Failed mirrors the completed case
// for the `failed` source status.
func TestRunResumeVerify_HappyPath_Failed(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
		task.VerifyResumeSession = "sess-x"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	if err := RunResumeVerify(context.Background(), ResumeVerifyOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumeVerify: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--phase=verify-only", "--interactive=true",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
}

func TestResumeVerify_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "resume-verify" {
			return
		}
	}
	t.Fatal("`j tasks resume-verify` should be registered as a child of `j tasks`")
}

func TestFilterTasksWithVerifySession(t *testing.T) {
	rows := []tasks.Task{
		{ID: "a", VerifyResumeSession: "x"},
		{ID: "b", VerifyResumeSession: ""},
		{ID: "c", VerifyResumeSession: "y"},
	}
	got := filterTasksWithVerifySession(rows)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("filtered = %+v, want [a c]", got)
	}
}
