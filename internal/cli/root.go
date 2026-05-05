// Package cli is the cobra entry point for the `j` binary. Each
// subcommand lives in its own sub-package (plan, run, web); root.go
// just stitches them together.
package cli

import (
	"errors"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/cli/initcmd"
	"github.com/spacelions/j/internal/cli/run"
	"github.com/spacelions/j/internal/cli/settings"
	"github.com/spacelions/j/internal/cli/tasks"
	"github.com/spacelions/j/internal/cli/verify"
	"github.com/spacelions/j/internal/cli/web"
	"github.com/spacelions/j/internal/cli/work"
)

// Execute is the process entry point. It builds the cobra root, parses
// os.Args, and returns the exit code.
func Execute() int {
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
		work.New(),
		verify.New(),
		settings.New(),
		tasks.New(),
		initcmd.New(),
	)
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
