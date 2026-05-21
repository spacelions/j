// Package settings implements the `j settings` subcommand. It owns
// the on-disk bbolt database under `<cwd>/.j/settings`. Listing is
// done with plain `j settings`. Values are set with
// `j settings set bucket.key=value [bucket.key=value ...]` (e.g.
// `j settings set planner.tool=cursor planner.model=opus`).
//
// Setting `planner.prompt`, `worker.prompt`, or `verifier.prompt` to
// a path stores the path AND, if the file does not yet exist,
// seeds it with the bundled role markdown (parents created). The
// rendered prompt at runtime then comes from the file instead of
// the embedded default. Existing files are never overwritten.
//
// Values are cleared with `j settings reset`, which accepts:
//   - no args  → prompt + wipe the entire `.j/` directory.
//   - bucket   → wipe every key under that bucket (`reset planner`).
//   - bucket.key → wipe one key (`reset planner.tool`).
//   - any number of the above, whitespace-separated and applied in
//     left-to-right order (`reset planner worker.model verifier`).
//
// Resetting a `<role>.prompt` removes the override but does NOT
// delete the on-disk markdown copy.
//
// Whitespace is the only target separator: `,` and `;` are NOT
// recognized and remain part of the literal target name.
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
		Long: "Manages the on-disk settings database used by `j` to persist " +
			"user preferences (e.g. the planner or worker tool/model last " +
			"selected). The DB " +
			"lives at <cwd>/.j/settings. The file is created by `j init`; the " +
			"settings subcommands assume it already exists (a missing file makes the " +
			"shared pre-flight prompt the user to run init).\n\n" +
			"Setting planner.prompt / worker.prompt / verifier.prompt to a path " +
			"records the override AND, if the file does not yet exist, seeds it " +
			"with the bundled role markdown so users can edit a real file. The " +
			"override is honoured by every backend (shell-out and ADK).",
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
	path := store.DefaultPath()
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return fn(path, s)
}
