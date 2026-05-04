package work

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

// TestNew_ToolFlag_DefaultsEmpty asserts the new --tool flag.
func TestNew_ToolFlag_DefaultsEmpty(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	f := cmd.Flags().Lookup("tool")
	if f == nil {
		t.Fatal("--tool flag was not registered")
	}
	if f.DefValue != "" {
		t.Fatalf("--tool default = %q, want empty", f.DefValue)
	}
	if viper.GetString("work.tool") != "" {
		t.Error("work.tool should default to empty via BindPFlag")
	}
}

// TestNew_ModelFlag_DefaultsEmpty asserts the new --model flag.
func TestNew_ModelFlag_DefaultsEmpty(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	f := cmd.Flags().Lookup("model")
	if f == nil {
		t.Fatal("--model flag was not registered")
	}
	if f.DefValue != "" {
		t.Fatalf("--model default = %q, want empty", f.DefValue)
	}
}

// TestNew_ToolFlag_FlowsToViper confirms BindPFlag wiring round-trip.
func TestNew_ToolFlag_FlowsToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if err := cmd.Flags().Set("tool", "claude"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("work.tool"); got != "claude" {
		t.Errorf("work.tool = %q, want claude", got)
	}
	if err := cmd.Flags().Set("model", "opus"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("work.model"); got != "opus" {
		t.Errorf("work.model = %q, want opus", got)
	}
}

// TestNew_ToolEnv covers WORK_TOOL / WORK_MODEL bindings.
func TestNew_ToolEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("WORK_TOOL", "claude")
	t.Setenv("WORK_MODEL", "opus")

	_ = New()
	if got := viper.GetString("work.tool"); got != "claude" {
		t.Errorf("work.tool = %q, want claude", got)
	}
	if got := viper.GetString("work.model"); got != "opus" {
		t.Errorf("work.model = %q, want opus", got)
	}
}

// TestNew_FromTaskFlowsToViper confirms BindPFlag wires the renamed
// --from-task flag into the work.from_task viper key.
func TestNew_FromTaskFlowsToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if err := cmd.Flags().Set("from-task", "abc"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("work.from_task"); got != "abc" {
		t.Errorf("work.from_task = %q, want %q", got, "abc")
	}
}

// TestNew_FromTaskEnv covers the env-var binding so WORK_FROM_TASK
// can be used from CI without a flag.
func TestNew_FromTaskEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("WORK_FROM_TASK", "abc")

	_ = New()
	if got := viper.GetString("work.from_task"); got != "abc" {
		t.Errorf("WORK_FROM_TASK=abc should make work.from_task = %q, got %q", "abc", got)
	}
}

func TestNew_Smoke(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if cmd == nil {
		t.Fatal("New returned nil")
	}
	if cmd.Use != "work" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "work")
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil")
	}
	if f := cmd.Flags().Lookup("from-file"); f != nil {
		t.Error("--from-file flag should not be registered")
	}
	if f := cmd.Flags().Lookup("target"); f != nil {
		t.Error("--target should no longer be registered after rename")
	}
	if f := cmd.Flags().Lookup("from-task"); f == nil {
		t.Error("--from-task flag was not registered")
	}
	if f := cmd.Flags().Lookup("interactive"); f == nil {
		t.Error("--interactive flag was not registered")
	}
	if err := cmd.Flags().Set("interactive", "false"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if viper.GetBool("work.interactive") {
		t.Error("work.interactive should be false after setting --interactive=false")
	}
}

// TestNew_RunE_PropagatesWorkError invokes the RunE closure inside the
// same package so its body (calling Run with an Options built from
// viper + cursor.New()) is exercised by work_test coverage. We use a
// missing --from-task id so resolution short-circuits before any agent
// or UI is touched.
func TestNew_RunE_PropagatesWorkError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	mustInit(t)

	cmd := New()
	if err := cmd.Flags().Set("from-task", "missing"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(context.Background())

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error from missing --from-task id")
	}
}

// TestNew_YesFlag_DefaultsFalse pins the new --yes flag's default
// (status-mismatch prompts are required by default) and the Viper
// round-trip when the user opts in.
func TestNew_YesFlag_DefaultsFalse(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	f := cmd.Flags().Lookup("yes")
	if f == nil {
		t.Fatal("--yes flag was not registered")
	}
	if f.DefValue != "false" {
		t.Fatalf("--yes default = %q, want false", f.DefValue)
	}
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if !viper.GetBool("work.yes") {
		t.Fatal("work.yes should be true after setting --yes=true")
	}
}

// TestNew_YesEnv covers WORK_YES binding so CI / scripts can skip
// the status-mismatch prompt without a flag.
func TestNew_YesEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("WORK_YES", "1")

	_ = New()
	if !viper.GetBool("work.yes") {
		t.Fatal("work.yes should be true when WORK_YES=1")
	}
}
