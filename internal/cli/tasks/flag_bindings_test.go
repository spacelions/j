package tasks

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestExplicitBool_FlagSetFalseIsExplicit(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := &cobra.Command{}
	cmd.Flags().Bool("yes", false, "")
	bindFlagEnv(cmd, bindEnv("test.yes", "yes", "TEST_YES"))
	if err := cmd.Flags().Set("yes", "false"); err != nil {
		t.Fatal(err)
	}
	if explicitBool(cmd, "yes", "test.yes", "TEST_YES") {
		t.Fatal("explicitBool = true, want explicit false")
	}
}
