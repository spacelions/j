// Package cli is the cobra entry point for the `j` binary. Each
// subcommand lives in its own sub-package (plan, run, web); root.go
// just stitches them together.
package cli

import (
	"errors"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/spf13/viper"

	"github.com/spacelions/j/internal/cli/completion"
	"github.com/spacelions/j/internal/cli/initcmd"
	"github.com/spacelions/j/internal/cli/run"
	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/cli/version"
	"github.com/spacelions/j/internal/cli/web"
	"github.com/spacelions/j/internal/lifecycle"
)

// NewRoot builds the cobra root command with every subcommand wired
// in. Reused by Execute() and by tests that need to drive the root
// command in-process.
func NewRoot() *cobra.Command {
	lifecycle.Init()
	lifecycle.InitLinearPush()
	lifecycle.InitLinearStateSync()
	lifecycle.InitLinearTitleSync()
	lifecycle.InitLinearVerifyPush()
	viper.SetEnvPrefix("J")
	viper.AutomaticEnv()
	root := &cobra.Command{
		Use:   "j",
		Short: "J Harness CLI",
		// Errors are printed once here; don't let cobra print them too.
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(
		run.New(),
		web.New(),
		settings.New(),
		tasks.New(),
		initcmd.New(),
		version.New(),
	)
	root.AddCommand(completion.New(root))
	return root
}

// Execute is the process entry point. It builds the cobra root, parses
// os.Args, and returns the exit code.
func Execute() int {
	root := NewRoot()
	root.SetArgs(os.Args[1:])
	if err := root.Execute(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return 0
		}
		_, _ = uitheme.DangerousFprintf(os.Stderr, "J: %v\n", err)
		return 1
	}
	return 0
}
