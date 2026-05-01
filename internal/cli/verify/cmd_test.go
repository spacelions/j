package verify

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

func TestNew_FromSettingsFlag_DefaultsTrue(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	f := cmd.Flags().Lookup("from-settings")
	if f == nil {
		t.Fatal("--from-settings flag was not registered")
	}
	if f.DefValue != "true" {
		t.Fatalf("--from-settings default = %q, want %q", f.DefValue, "true")
	}
	if !viper.GetBool("verify.from_settings") {
		t.Error("verify.from_settings should default to true via BindPFlag")
	}
}

func TestNew_FromSettingsFlag_FalseFlowsToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if err := cmd.Flags().Set("from-settings", "false"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if viper.GetBool("verify.from_settings") {
		t.Error("verify.from_settings should be false after --from-settings=false")
	}
}

func TestNew_FromSettingsEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("VERIFY_FROM_SETTINGS", "false")

	_ = New()
	if viper.GetBool("verify.from_settings") {
		t.Error("VERIFY_FROM_SETTINGS=false should make verify.from_settings false")
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
	for _, name := range []string{"from-task", "interactive", "from-settings", "max-iterations"} {
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
