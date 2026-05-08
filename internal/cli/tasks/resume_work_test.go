package tasks

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

func seedWorkingTaskWithWorkSession(t *testing.T) string {
	t.Helper()
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusWorking
		task.WorkTool = "cursor"
		task.WorkModel = "sonnet-4"
		task.WorkResumeSession = "work-cursor-session"
	})
	return id
}

func TestRunResumeWork_NoActiveSession(t *testing.T) {
	setupContinueEnv(t)
	var stdout bytes.Buffer
	err := RunResumeWork(t.Context(), ResumeWorkOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     &fakeUI{},
	})
	if err != nil {
		t.Fatalf("RunResumeWork: %v", err)
	}
	if !strings.Contains(stdout.String(), noActiveWorkSessionMessage) {
		t.Fatalf("stdout = %q, want %q", stdout.String(), noActiveWorkSessionMessage)
	}
}

func TestRunResumeWork_HappyPath(t *testing.T) {
	setupContinueEnv(t)
	id := seedWorkingTaskWithWorkSession(t)
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	err := RunResumeWork(t.Context(), ResumeWorkOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	})
	if err != nil {
		t.Fatalf("RunResumeWork: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	wantArgs := []string{"tasks", "orchestrate", "--id", id, "--phase=from-work", "--interactive=true"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v", args, wantArgs)
	}
}

func TestRunResumeWork_PickerAbort(t *testing.T) {
	setupContinueEnv(t)
	seedWorkingTaskWithWorkSession(t)
	seedWorkingTaskWithWorkSession(t)
	ui := &fakeUI{} // empty pickReturn -> ok=false
	if err := RunResumeWork(t.Context(), ResumeWorkOptions{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Agents: []codingagents.Agent{newContinueAgent()},
		UI:     ui,
	}); err != nil {
		t.Fatalf("RunResumeWork: %v", err)
	}
	if ui.pickCalls != 1 {
		t.Fatalf("PickTask calls = %d, want 1", ui.pickCalls)
	}
}

func TestRunResumeWork_SpawnFails(t *testing.T) {
	setupContinueEnv(t)
	id := seedWorkingTaskWithWorkSession(t)
	ui := &fakeUI{pickReturn: id}
	err := RunResumeWork(t.Context(), ResumeWorkOptions{
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
}

func TestRunResumeWork_AppliesDefaults(t *testing.T) {
	// withDefaults is now on resumeOptions; RunResumeWork wraps it
	// so calling with nil opts is the easiest way to exercise the path.
	setupContinueEnv(t)
	if err := RunResumeWork(t.Context(), ResumeWorkOptions{
		Agents: []codingagents.Agent{newContinueAgent()},
	}); err != nil {
		t.Fatalf("RunResumeWork: %v", err)
	}
}

func TestFilterTasksWithWorkSession(t *testing.T) {
	rows := []tasks.Task{
		{ID: "a", WorkResumeSession: "s1"},
		{ID: "b", WorkResumeSession: ""},
		{ID: "c", WorkResumeSession: "s2"},
	}
	got := filterTasksBySession(rows, resumeWorkConfig.hasSession)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("filtered IDs = %v, want [a c]", []string{got[0].ID, got[1].ID})
	}
}

// TestRunResumeWork_HappyPath_Completed pins that resume-work
// succeeds for a completed row carrying a work resume session. The
// FSM edge {completed, EventWorkResume, working} must permit it.
func TestRunResumeWork_HappyPath_Completed(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
		task.WorkResumeSession = "sess-x"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	if err := RunResumeWork(t.Context(), ResumeWorkOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumeWork: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--phase=from-work", "--interactive=true",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
}

// TestRunResumeWork_HappyPath_Failed mirrors the completed case for
// the `failed` source status.
func TestRunResumeWork_HappyPath_Failed(t *testing.T) {
	setupContinueEnv(t)
	id := seedTaskFull(t, func(task *tasks.Task) {
		task.Status = tasks.StatusFailed
		task.WorkResumeSession = "sess-x"
	})
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	ui := &fakeUI{pickReturn: id}
	if err := RunResumeWork(t.Context(), ResumeWorkOptions{
		Stdin:   strings.NewReader(""),
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Agents:  []codingagents.Agent{newContinueAgent()},
		UI:      ui,
		JBinary: argvJBinary(t, argvPath),
	}); err != nil {
		t.Fatalf("RunResumeWork: %v", err)
	}
	args := readSpawnedArgv(t, argvPath)
	want := []string{
		"tasks", "orchestrate", "--id", id,
		"--phase=from-work", "--interactive=true",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", args, want)
	}
}

func TestRunResumeWork_RegisteredAsChild(t *testing.T) {
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "resume-work" {
			return
		}
	}
	t.Fatal("`j tasks resume-work` should be registered as a child of `j tasks`")
}

func TestNewResumeWorkCmd_RunE_NoTasks(t *testing.T) {
	setupContinueEnv(t)
	cmd := newResumeWorkCmd()
	cmd.SetContext(t.Context())
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !strings.Contains(stdout.String(), noActiveWorkSessionMessage) {
		t.Fatalf("stdout = %q, want %q",
			stdout.String(), noActiveWorkSessionMessage)
	}
}
