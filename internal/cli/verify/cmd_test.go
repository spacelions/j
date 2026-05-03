package verify

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
	if viper.GetString("verify.tool") != "" {
		t.Error("verify.tool should default to empty via BindPFlag")
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
	if got := viper.GetString("verify.tool"); got != "claude" {
		t.Errorf("verify.tool = %q, want claude", got)
	}
	if err := cmd.Flags().Set("model", "opus"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("verify.model"); got != "opus" {
		t.Errorf("verify.model = %q, want opus", got)
	}
}

// TestNew_ToolEnv covers VERIFY_TOOL / VERIFY_MODEL bindings.
func TestNew_ToolEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("VERIFY_TOOL", "claude")
	t.Setenv("VERIFY_MODEL", "opus")

	_ = New()
	if got := viper.GetString("verify.tool"); got != "claude" {
		t.Errorf("verify.tool = %q, want claude", got)
	}
	if got := viper.GetString("verify.model"); got != "opus" {
		t.Errorf("verify.model = %q, want opus", got)
	}
}


func TestNew_FromTaskFlowsToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if err := cmd.Flags().Set("from-task", "abc"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetString("verify.from_task"); got != "abc" {
		t.Errorf("verify.from_task = %q, want %q", got, "abc")
	}
}

func TestNew_FromTaskEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("VERIFY_FROM_TASK", "abc")

	_ = New()
	if got := viper.GetString("verify.from_task"); got != "abc" {
		t.Errorf("VERIFY_FROM_TASK=abc should set verify.from_task = %q, got %q", "abc", got)
	}
}

func TestNew_MaxIterations_Default(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	f := cmd.Flags().Lookup("max-iterations")
	if f == nil {
		t.Fatal("--max-iterations flag was not registered")
	}
	if f.DefValue != "3" {
		t.Fatalf("--max-iterations default = %q, want %q", f.DefValue, "3")
	}
	if got := viper.GetInt("verify.max_iterations"); got != 3 {
		t.Fatalf("verify.max_iterations = %d, want 3", got)
	}
}

func TestNew_MaxIterations_FlagFlowsToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if err := cmd.Flags().Set("max-iterations", "5"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if got := viper.GetInt("verify.max_iterations"); got != 5 {
		t.Errorf("verify.max_iterations = %d, want 5", got)
	}
}

func TestNew_MaxIterations_Env(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("VERIFY_MAX_ITERATIONS", "7")

	_ = New()
	if got := viper.GetInt("verify.max_iterations"); got != 7 {
		t.Errorf("VERIFY_MAX_ITERATIONS=7 should set verify.max_iterations = %d, got %d", 7, got)
	}
}

func TestNew_Smoke(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if cmd == nil {
		t.Fatal("New returned nil")
	}
	if cmd.Use != "verify" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "verify")
	}
	if cmd.RunE == nil {
		t.Fatal("RunE is nil")
	}
	for _, name := range []string{"from-task", "interactive", "tool", "model", "max-iterations"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("--%s flag was not registered", name)
		}
	}
	if err := cmd.Flags().Set("interactive", "false"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if viper.GetBool("verify.interactive") {
		t.Error("verify.interactive should be false after setting --interactive=false")
	}
}

// TestNew_RunE_PropagatesError invokes the RunE closure inside the
// same package so its body is exercised by verify_test coverage. We
// use a bogus --from-task id so resolveTask short-circuits before
// any agent or UI is touched.
func TestNew_RunE_PropagatesError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Chdir(t.TempDir())
	mustInit(t)

	cmd := New()
	if err := cmd.Flags().Set("from-task", "this-id-does-not-exist"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(context.Background())

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error from bogus --from-task id")
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
	if !viper.GetBool("verify.yes") {
		t.Fatal("verify.yes should be true after setting --yes=true")
	}
}

// TestNew_YesEnv covers VERIFY_YES binding so CI / scripts can skip
// the status-mismatch prompt without a flag.
func TestNew_YesEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("VERIFY_YES", "1")

	_ = New()
	if !viper.GetBool("verify.yes") {
		t.Fatal("verify.yes should be true when VERIFY_YES=1")
	}
}
