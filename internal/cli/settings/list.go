package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/store"
)

func runList(cmd *cobra.Command) error {
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(cmd.OutOrStdout(), "no settings stored")
			return nil
		}
		return err
	}

	return withOpenStore(func(_ string, s *store.Store) error {
		empty, err := s.IsEmpty()
		if err != nil {
			return err
		}
		if empty {
			fmt.Fprintln(cmd.OutOrStdout(), "no settings stored")
			return nil
		}
		buckets, err := s.ListBuckets()
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		for _, b := range buckets {
			entries, err := s.List(b)
			if err != nil {
				return err
			}
			for _, kv := range entries {
				fmt.Fprintf(out, "%s.%s = %s\n", b, kv.Key, kv.Value)
			}
		}
		return nil
	})
}
