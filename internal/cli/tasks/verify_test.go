package tasks

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// TestNewRedoVerifyCmd_FlagDefaults pins the registered flag set
// and defaults for `j tasks verify`.
func TestNewRedoVerifyCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRedoVerifyCmd()
	if cmd.Use != "verify" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	want := []string{"from-task", "interactive"}
	got := flagNames(cmd)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("flags = %v, want %v", got, want)
	}
}

// TestNewRedoVerifyCmd_FlagsBindToViper covers the --from-task /
// --interactive viper bindings.
func TestNewRedoVerifyCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRedoVerifyCmd()
	if err := cmd.Flags().Set("from-task", "abc"); err != nil {
		t.Fatalf("Flags().Set from-task: %v", err)
	}
	if got := viper.GetString("tasks.verify.from_task"); got != "abc" {
		t.Errorf("tasks.verify.from_task = %q", got)
	}
	if err := cmd.Flags().Set("interactive", "false"); err != nil {
		t.Fatalf("Flags().Set interactive: %v", err)
	}
	if viper.GetBool("tasks.verify.interactive") {
		t.Errorf("tasks.verify.interactive = true, want false")
	}
}

// TestNewRedoVerifyCmd_EnvBindings covers the TASKS_VERIFY_* env
// vars.
func TestNewRedoVerifyCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_VERIFY_FROM_TASK", "env-id")
	t.Setenv("TASKS_VERIFY_INTERACTIVE", "false")
	_ = newRedoVerifyCmd()
	if got := viper.GetString("tasks.verify.from_task"); got != "env-id" {
		t.Errorf("tasks.verify.from_task = %q", got)
	}
	if viper.GetBool("tasks.verify.interactive") {
		t.Errorf("tasks.verify.interactive = true, want false")
	}
}

// TestNewRedoVerifyCmd_RunE_MissingTask exercises the RunE closure.
func TestNewRedoVerifyCmd_RunE_MissingTask(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	installRedoStubs(t)
	cmd := newRedoVerifyCmd()
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

// TestRedoVerify_RegisteredAsChild verifies wiring on the parent.
func TestRedoVerify_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "verify" {
			return
		}
	}
	t.Fatal("`j tasks verify` should be registered as a child of `j tasks`")
}
