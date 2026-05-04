package tasks

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// TestNewRedoPlanCmd_FlagDefaults pins the registered flag set,
// defaults, and viper bindings for `j tasks plan`.
func TestNewRedoPlanCmd_FlagDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRedoPlanCmd()
	if cmd.Use != "plan" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	want := []string{"from-task", "interactive"}
	got := flagNames(cmd)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("flags = %v, want %v", got, want)
	}
	interactive, err := cmd.Flags().GetBool("interactive")
	if err != nil {
		t.Fatalf("GetBool interactive: %v", err)
	}
	if !interactive {
		t.Fatalf("--interactive default = false, want true")
	}
}

// TestNewRedoPlanCmd_FlagsBindToViper covers the --from-task and
// --interactive viper bindings.
func TestNewRedoPlanCmd_FlagsBindToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRedoPlanCmd()
	if err := cmd.Flags().Set("from-task", "abc"); err != nil {
		t.Fatalf("Flags().Set from-task: %v", err)
	}
	if got := viper.GetString("tasks.plan.from_task"); got != "abc" {
		t.Errorf("tasks.plan.from_task = %q", got)
	}
	if err := cmd.Flags().Set("interactive", "false"); err != nil {
		t.Fatalf("Flags().Set interactive: %v", err)
	}
	if viper.GetBool("tasks.plan.interactive") {
		t.Errorf("tasks.plan.interactive = true, want false")
	}
}

// TestNewRedoPlanCmd_EnvBindings covers the TASKS_PLAN_* env vars.
func TestNewRedoPlanCmd_EnvBindings(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_PLAN_FROM_TASK", "env-id")
	t.Setenv("TASKS_PLAN_INTERACTIVE", "false")
	_ = newRedoPlanCmd()
	if got := viper.GetString("tasks.plan.from_task"); got != "env-id" {
		t.Errorf("tasks.plan.from_task = %q", got)
	}
	if viper.GetBool("tasks.plan.interactive") {
		t.Errorf("tasks.plan.interactive = true, want false")
	}
}

// TestNewRedoPlanCmd_RunE_MissingTask exercises the RunE closure end
// to end. With no list.db on disk, the command short-circuits with
// the empty message and returns nil; this proves the closure
// constructed RedoOptions and reached runRedo.
func TestNewRedoPlanCmd_RunE_MissingTask(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	installRedoStubs(t)
	cmd := newRedoPlanCmd()
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

// TestRedoPlan_RegisteredAsChild verifies wiring on the parent.
func TestRedoPlan_RegisteredAsChild(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	parent := New()
	for _, sub := range parent.Commands() {
		if sub.Name() == "plan" {
			return
		}
	}
	t.Fatal("`j tasks plan` should be registered as a child of `j tasks`")
}

// TestResolveRedoInteractive_DefaultsTrue pins the default-true
// branch of resolveRedoInteractive: with --interactive unset and
// the env var unset, the helper returns true.
func TestResolveRedoInteractive_DefaultsTrue(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRedoPlanCmd()
	if !resolveRedoInteractive(cmd, "tasks.plan.interactive", "TASKS_PLAN_INTERACTIVE") {
		t.Fatal("resolveRedoInteractive defaulted to false; want true")
	}
}

// TestResolveRedoInteractive_FlagOverridesDefault: --interactive=false
// flips the result to false.
func TestResolveRedoInteractive_FlagOverridesDefault(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	cmd := newRedoPlanCmd()
	if err := cmd.Flags().Set("interactive", "false"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if resolveRedoInteractive(cmd, "tasks.plan.interactive", "TASKS_PLAN_INTERACTIVE") {
		t.Fatal("resolveRedoInteractive returned true; want false")
	}
}

// TestResolveRedoInteractive_EnvOverridesDefault: env var alone is
// enough to enter the explicit branch and read the viper value.
func TestResolveRedoInteractive_EnvOverridesDefault(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("TASKS_PLAN_INTERACTIVE", "false")
	cmd := newRedoPlanCmd()
	if resolveRedoInteractive(cmd, "tasks.plan.interactive", "TASKS_PLAN_INTERACTIVE") {
		t.Fatal("resolveRedoInteractive returned true; want false (env should override)")
	}
}

func flagNames(cmd interface {
	Flags() *pflag.FlagSet
}) []string {
	var names []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
	return names
}
