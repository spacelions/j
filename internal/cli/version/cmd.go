// Package version implements the `j version` subcommand.
package version

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is injected at build time via -ldflags (see Makefile build target).
var Version = "dev"

// New returns the version cobra command.
func New() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the j version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), Version)
			return nil
		},
	}
}
