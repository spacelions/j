// Package settings implements the `j settings` subcommand. It owns
// the on-disk bbolt database under `<cwd>/.j/settings`. Listing is
// done with plain `j settings`. Values are set with
// `j settings set bucket.key=value [bucket.key=value ...]` (e.g.
// `j settings set planner.tool=cursor planner.model=opus`) and
// cleared with `j settings reset` (all) or
// `j settings reset bucket.key`.
package settings

import (
	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/preflight"
	"github.com/spacelions/j/internal/store"
)

// New returns the `j settings` cobra command tree.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "List, set, or reset local j settings",
		Long: "Manages the on-disk settings database used by `j` to persist user " +
			"preferences (e.g. the planner or coder tool/model last selected). The DB " +
			"lives at <cwd>/.j/settings. The file is created by `j init`; the " +
			"settings subcommands assume it already exists (a missing file makes the " +
			"shared pre-flight prompt the user to run init).",
		PersistentPreRunE: preflight.PreRunE,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd)
		},
	}
	cmd.AddCommand(
		newSetCmd(),
		newResetCmd(),
	)
	return cmd
}

// withOpenStore resolves the default DB path, opens it, and invokes fn
// with the result. The store is closed before withOpenStore returns,
// regardless of fn's outcome.
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
