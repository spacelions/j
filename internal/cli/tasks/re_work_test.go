package tasks

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

func TestRunReWork_NoTasks(t *testing.T) {
	setupContinueEnv(t)
	ui := &fakeUI{}
	var stdout bytes.Buffer
	err := RunReWork(context.Background(), ReWorkOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	})
	if err != nil {
		t.Fatalf("RunReWork: %v", err)
	}
	if !strings.Contains(stdout.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), emptyMessage)
	}
}

func TestRunReWork_FromTaskNotFound(t *testing.T) {
	setupContinueEnv(t)
	err := RunReWork(context.Background(), ReWorkOptions{
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

func TestRunReWork_StatusOverrideDeclined(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorking
	})
	ui := &fakeUI{statusReturn: false}
	if err := RunReWork(context.Background(), ReWorkOptions{
		FromTask: id,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newContinueAgent()},
		UI:       ui,
	}); err != nil {
		t.Fatalf("RunReWork: %v", err)
	}
	if ui.statusCalls != 1 {
		t.Fatalf("statusCalls = %d, want 1", ui.statusCalls)
	}
}

func TestRunReWork_HappyPath(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil) // plan-done
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	if err := RunReWork(context.Background(), ReWorkOptions{
		FromTask: id,
		Stdin:    strings.NewReader(""),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Agents:   []codingagents.Agent{newContinueAgent()},
		UI:       &fakeUI{},
		Interactive: boolPtr(false),
		JBinary:  argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunReWork: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{"tasks", "orchestrate", "--id", id, "--phase=from-work", "--interactive=false"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
}

func TestRunReWork_InteractiveRunsInline(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	err := RunReWork(context.Background(), ReWorkOptions{
		FromTask:    id,
		Interactive: boolPtr(true),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          &fakeUI{},
		JBinary:     argvJBinary(t, argvPath),
	})
	if err != nil {
		t.Fatalf("RunReWork: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	if !containsArg(args, "--interactive=true") {
		t.Fatalf("argv = %v, want --interactive=true", args)
	}
}

func TestRunReWork_SpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, nil)
	err := RunReWork(context.Background(), ReWorkOptions{
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

func TestRunReWork_AppliesDefaults(t *testing.T) {
	opts := ReWorkOptions{}
	opts = opts.withDefaults()
	if opts.Stdin == nil || opts.Stdout == nil || opts.Stderr == nil {
		t.Fatal("withDefaults should fill nil streams")
	}
	if opts.UI == nil {
		t.Fatal("withDefaults should give default UI")
	}
}

func TestNewReWorkCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newReWorkCmd()
	if cmd.Use != "re-work" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	want := []string{"from-task", "interactive", "model", "tool"}
	if len(names) != len(want) {
		t.Fatalf("flags = %v, want %v", names, want)
	}
}

func TestNewReWorkCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newReWorkCmd()
	if err := cmd.Flags().Set("from-task", "abc"); err != nil {
		t.Fatal(err)
	}
	if got := viper.GetString("tasks.rework.from_task"); got != "abc" {
		t.Errorf("tasks.rework.from_task = %q", got)
	}
}

func TestNewReWorkCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_REWORK_FROM_TASK", "env-id")
	t.Setenv("TASKS_REWORK_TOOL", "cursor")
	t.Setenv("TASKS_REWORK_MODEL", "sonnet")
	_ = newReWorkCmd()
	if got := viper.GetString("tasks.rework.from_task"); got != "env-id" {
		t.Errorf("tasks.rework.from_task = %q", got)
	}
	if got := viper.GetString("tasks.rework.tool"); got != "cursor" {
		t.Errorf("tasks.rework.tool = %q", got)
	}
	if got := viper.GetString("tasks.rework.model"); got != "sonnet" {
		t.Errorf("tasks.rework.model = %q", got)
	}
}

// TestRunReWork_FromCompletedSpawnsAfterConfirm pins that re-work
// no longer rejects a completed task at the IsLegal guard.
func TestRunReWork_FromCompletedSpawnsAfterConfirm(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{statusReturn: true}
	if err := RunReWork(context.Background(), ReWorkOptions{
		FromTask:    id,
		Interactive: boolPtr(false),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          ui,
		JBinary:     argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunReWork: %v", err)
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

// TestRunReWork_FromFailedSpawnsAfterConfirm mirrors the completed
// case for the `failed` source status.
func TestRunReWork_FromFailedSpawnsAfterConfirm(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{statusReturn: true}
	if err := RunReWork(context.Background(), ReWorkOptions{
		FromTask:    id,
		Interactive: boolPtr(false),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
		Stderr:      io.Discard,
		Agents:      []codingagents.Agent{newContinueAgent()},
		UI:          ui,
		JBinary:     argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunReWork: %v", err)
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

func TestRunReWork_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "re-work" {
			return
		}
	}
	t.Fatal("`j tasks re-work` should be registered as a child of `j tasks`")
}

func TestClearWorkResumeSession_EmptySessionReturnsNil(t *testing.T) {
	setupContinueEnv(t)
	// seedTaskFull sets PlanResumeSession only; WorkResumeSession is empty.
	id := seedTaskFull(t, nil)
	if err := clearWorkResumeSession(id); err != nil {
		t.Fatalf("clearWorkResumeSession: %v", err)
	}
}

func TestClearWorkResumeSession_PopulatedSessionClears(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.WorkResumeSession = "session-xyz"
	})
	if err := clearWorkResumeSession(id); err != nil {
		t.Fatalf("clearWorkResumeSession: %v", err)
	}
	// Verify the session was cleared.
	s, err := tasks.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	row, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if row.WorkResumeSession != "" {
		t.Fatalf("WorkResumeSession = %q, want empty",
			row.WorkResumeSession)
	}
}

func TestClearWorkResumeSession_UnknownID(t *testing.T) {
	setupContinueEnv(t)
	err := clearWorkResumeSession("ghost-id")
	if err == nil {
		t.Fatal("expected error for unknown task id")
	}
}

func TestNewReWorkCmd_RunE_NoTasks(t *testing.T) {
	setupContinueEnv(t)
	cmd := newReWorkCmd()
	cmd.SetContext(context.Background())
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(stdout.String(), emptyMessage) {
		t.Fatalf("stdout = %q, want %q",
			stdout.String(), emptyMessage)
	}
}

func TestNewReWorkCmd_RunE_InteractiveFlag(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setupContinueEnv(t)
	cmd := newReWorkCmd()
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
