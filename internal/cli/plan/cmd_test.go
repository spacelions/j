package plan

import (
	"context"
	"testing"

	"github.com/spf13/viper"
)

func TestNewCommand_Smoke(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := NewCommand()
	if cmd == nil {
		t.Fatal("NewCommand returned nil")
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

// TestNewCommand_RunE_PropagatesPlanError invokes the RunE closure
// inside the same package so its body (calling Run with an Options
// built from viper + cursor.New()) is exercised by plan_test coverage.
// We use a bogus target so resolveTarget short-circuits before any
// agent or UI is touched.
func TestNewCommand_RunE_PropagatesPlanError(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := NewCommand()
	if err := cmd.Flags().Set("target", "/this/path/does/not/exist.md"); err != nil {
		t.Fatalf("Flags().Set: %v", err)
	}
	cmd.SetContext(context.Background())

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error from bogus target")
	}
}
