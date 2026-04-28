// Package settings implements the `j settings` subcommand. It owns
// the on-disk bbolt database under `<cwd>/.j/settings` and exposes
// `init` to create it and `show` to print stored values. Bare
// `j settings` is a synonym for `j settings show`.
package settings

import (
	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/store"
)

// New returns the `j settings` cobra command tree.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Inspect or initialize the local settings database",
		Long: "Manages the on-disk settings database used by `j` to persist user " +
			"preferences (e.g. the planner tool/model last selected). The DB lives " +
			"at <cwd>/.j/settings and is created lazily by `j settings init`.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runShow(cmd)
		},
	}
	cmd.AddCommand(
		newInitCmd(),
		newShowCmd(),
	)
	return cmd
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the settings database and its planner bucket",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd)
		},
	}
}

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print stored settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runShow(cmd)
		},
	}
}

// withOpenStore resolves the default DB path, opens it, and invokes fn
// with the result. The store is closed before withOpenStore returns,
// regardless of fn's outcome. Both error paths (path resolution and
// open) are exercised through the unit tests for runInit/runShow which
// use t.Chdir + filesystem-shape tricks to drive realistic failures.
func withOpenStore(fn func(path string, s *store.Store) error) error {
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return fn(path, s)
}
