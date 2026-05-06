package tasks

import (
	"bytes"
	"context"
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
	err := RunResumeWork(context.Background(), ResumeWorkOptions{
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
	err := RunResumeWork(context.Background(), ResumeWorkOptions{
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
	if err := RunResumeWork(context.Background(), ResumeWorkOptions{
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
	err := RunResumeWork(context.Background(), ResumeWorkOptions{
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
	opts := ResumeWorkOptions{}
	opts = opts.withDefaults()
	if opts.Stdin == nil || opts.Stdout == nil || opts.Stderr == nil {
		t.Fatal("withDefaults should fill nil streams")
	}
	if opts.UI == nil {
		t.Fatal("withDefaults should give default UI")
	}
}

func TestFilterTasksWithWorkSession(t *testing.T) {
	rows := []tasks.Task{
		{ID: "a", WorkResumeSession: "s1"},
		{ID: "b", WorkResumeSession: ""},
		{ID: "c", WorkResumeSession: "s2"},
	}
	got := filterTasksWithWorkSession(rows)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "c" {
		t.Fatalf("filtered IDs = %v, want [a c]", []string{got[0].ID, got[1].ID})
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
