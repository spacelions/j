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
	wantArgs := []string{"tasks", "orchestrate", "--id", id, "--skip-planning=true", "--interactive=false"}
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


