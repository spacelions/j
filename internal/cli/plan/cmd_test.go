package plan

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

// TestNew_ToolFlag_DefaultsEmpty asserts the new --tool flag is
// registered as an empty-string one-off override.
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
	if viper.GetString("plan.tool") != "" {
		t.Error("plan.tool should default to empty via BindPFlag")
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
	if got := viper.GetString("plan.tool"); got != "claude" {
		t.Errorf("plan.tool = %q, want claude", got)
	}
	if err := cmd.Flags().Set("model", "opus"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("plan.model"); got != "opus" {
		t.Errorf("plan.model = %q, want opus", got)
	}
}

// TestNew_ToolEnv covers PLAN_TOOL / PLAN_MODEL bindings so CI can
// drive the override without a flag.
func TestNew_ToolEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("PLAN_TOOL", "claude")
	t.Setenv("PLAN_MODEL", "opus")

	_ = New()
	if got := viper.GetString("plan.tool"); got != "claude" {
		t.Errorf("plan.tool = %q, want claude", got)
	}
	if got := viper.GetString("plan.model"); got != "opus" {
		t.Errorf("plan.model = %q, want opus", got)
	}
}


func TestNew_Smoke(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if cmd == nil {
		t.Fatal("New returned nil")
	}
	if cmd.Use != "plan" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "plan")
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil")
	}
	if f := cmd.Flags().Lookup("from-file"); f == nil {
		t.Error("--from-file flag was not registered")
	}
	if cmd.Flags().Lookup("target") != nil {
		t.Error("--target flag should have been removed")
	}
	if f := cmd.Flags().Lookup("interactive"); f == nil {
		t.Error("--interactive flag was not registered")
	}
	// Round-trip via the flag's pointer to confirm BindPFlag wired the
	// flag value into the viper key.
	if err := cmd.Flags().Set("interactive", "false"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if viper.GetBool("plan.interactive") {
		t.Error("plan.interactive should be false after setting --interactive=false")
	}
}

// TestNew_RunE_PropagatesPlanError invokes the RunE closure inside the
// same package so its body (calling Run with an Options built from viper +
// cursor.New()) is exercised by plan_test coverage. We use a bogus
// from-file path so mdfile.Resolve short-circuits before any agent or
// UI is touched.
func TestNew_RunE_PropagatesPlanError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if err := cmd.Flags().Set("from-file", "/this/path/does/not/exist.md"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(context.Background())

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error from bogus from-file")
	}
}

// TestNew_FromTaskFlag_DefaultsEmpty pins the new --from-task flag's
// default value and Viper round-trip so users can supply
// PLAN_FROM_TASK / --from-task without surprise.
func TestNew_FromTaskFlag_DefaultsEmpty(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	f := cmd.Flags().Lookup("from-task")
	if f == nil {
		t.Fatal("--from-task flag was not registered")
	}
	if f.DefValue != "" {
		t.Fatalf("--from-task default = %q, want empty", f.DefValue)
	}
	if err := cmd.Flags().Set("from-task", "01ABCDEF"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("plan.from_task"); got != "01ABCDEF" {
		t.Fatalf("plan.from_task = %q, want 01ABCDEF", got)
	}
}

// TestNew_FromTaskEnv covers PLAN_FROM_TASK binding so CI can drive
// the re-plan flow without a flag.
func TestNew_FromTaskEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("PLAN_FROM_TASK", "01XYZ")

	_ = New()
	if got := viper.GetString("plan.from_task"); got != "01XYZ" {
		t.Fatalf("plan.from_task = %q, want 01XYZ", got)
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
	if !viper.GetBool("plan.yes") {
		t.Fatal("plan.yes should be true after setting --yes=true")
	}
}

// TestNew_YesEnv covers PLAN_YES binding.
func TestNew_YesEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("PLAN_YES", "1")

	_ = New()
	if !viper.GetBool("plan.yes") {
		t.Fatal("plan.yes should be true when PLAN_YES=1")
	}
}
