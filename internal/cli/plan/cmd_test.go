package plan

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
	if !viper.GetBool("plan.from_settings") {
		t.Error("plan.from_settings should default to true via BindPFlag")
	}
}

func TestNew_FromSettingsFlag_FalseFlowsToViper(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if err := cmd.Flags().Set("from-settings", "false"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	if viper.GetBool("plan.from_settings") {
		t.Error("plan.from_settings should be false after --from-settings=false")
	}
}

// TestNew_FromSettingsEnv covers the env-var binding so PLAN_FROM_SETTINGS
// can be used from CI without a flag.
func TestNew_FromSettingsEnv(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("PLAN_FROM_SETTINGS", "false")

	_ = New()
	if viper.GetBool("plan.from_settings") {
		t.Error("PLAN_FROM_SETTINGS=false should make plan.from_settings false")
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
	if f := cmd.Flags().Lookup("target"); f == nil {
		t.Error("--target flag was not registered")
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
// cursor.New()) is exercised by plan_test coverage. We use a bogus target
// so resolveTarget short-circuits before any agent or UI is touched.
func TestNew_RunE_PropagatesPlanError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := New()
	if err := cmd.Flags().Set("target", "/this/path/does/not/exist.md"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(context.Background())

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error from bogus target")
	}
}
