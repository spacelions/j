package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/store"
)

// runShow prints stored settings as `bucket.key = value` lines, sorted
// by bucket then key. When the DB does not yet exist it prints a hint
// telling the user to run `j settings init`. An existing-but-empty DB
// prints "no settings stored".
func runShow(cmd *cobra.Command) error {
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(cmd.OutOrStdout(), "no settings database found; run `j settings init` to create one")
			return nil
		}
		return err
	}

	return withOpenStore(func(_ string, s *store.Store) error {
		buckets, err := s.ListBuckets()
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		wrote := false
		for _, b := range buckets {
			entries, err := s.List(b)
			if err != nil {
				return err
			}
			for _, kv := range entries {
				fmt.Fprintf(out, "%s.%s = %s\n", b, kv.Key, kv.Value)
				wrote = true
			}
		}
		if !wrote {
			fmt.Fprintln(out, "no settings stored")
		}
		return nil
	})
}
