package settings

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/store"
)

// runInit resolves the default DB path, opens (creating directories as
// needed), ensures the planner bucket exists, and prints the resolved
// path. Re-running on an existing DB is a no-op.
func runInit(cmd *cobra.Command) error {
	return withOpenStore(func(path string, s *store.Store) error {
		if err := s.EnsureBucket(store.BucketPlanner); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), path)
		return nil
	})
}
