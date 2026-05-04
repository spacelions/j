package tasks

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestNewRedoWorkCmd_FlagDefaults pins the registered flag set and
// defaults for `j tasks work`.
func TestNewRedoWorkCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRedoWorkCmd()
	if cmd.Use != "work" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	want := []string{"from-task", "interactive"}
	got := flagNames(cmd)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("flags = %v, want %v", got, want)
	}
}

// TestNewRedoWorkCmd_FlagsBindToViper covers the --from-task /
// --interactive viper bindings.
func TestNewRedoWorkCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRedoWorkCmd()
	if err := cmd.Flags().Set("from-task", "abc"); err != nil {
		t.Fatalf("Flags().Set from-task: %v", err)
	}
	if got := viper.GetString("tasks.work.from_task"); got != "abc" {
		t.Errorf("tasks.work.from_task = %q", got)
	}
	if err := cmd.Flags().Set("interactive", "false"); err != nil {
		t.Fatalf("Flags().Set interactive: %v", err)
	}
	if viper.GetBool("tasks.work.interactive") {
		t.Errorf("tasks.work.interactive = true, want false")
	}
}

// TestNewRedoWorkCmd_EnvBindings covers the TASKS_WORK_* env vars.
func TestNewRedoWorkCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_WORK_FROM_TASK", "env-id")
	t.Setenv("TASKS_WORK_INTERACTIVE", "false")
	_ = newRedoWorkCmd()
	if got := viper.GetString("tasks.work.from_task"); got != "env-id" {
		t.Errorf("tasks.work.from_task = %q", got)
	}
	if viper.GetBool("tasks.work.interactive") {
		t.Errorf("tasks.work.interactive = true, want false")
	}
}

// TestNewRedoWorkCmd_RunE_MissingTask exercises the RunE closure.
func TestNewRedoWorkCmd_RunE_MissingTask(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	installRedoStubs(t)
	cmd := newRedoWorkCmd()
	cmd.SetContext(context.Background())
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

// TestRedoWork_RegisteredAsChild verifies wiring on the parent.
func TestRedoWork_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "work" {
			return
		}
	}
	t.Fatal("`j tasks work` should be registered as a child of `j tasks`")
}
