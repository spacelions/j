package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store"
)

// knownSectionOrder fixes the display order for the planner/worker/verifier
// pipeline plus the project bucket. These four sections always render even
// when empty so the layout is predictable.
var knownSectionOrder = []string{
	store.BucketProject,
	store.BucketPlanner,
	store.BucketWorker,
	store.BucketVerifier,
}

func runList(cmd *cobra.Command) error {
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			uitheme.NormalFprintln(cmd.OutOrStdout(), "J: no settings stored")
			return nil
		}
		return err
	}

	return withOpenStore(func(_ string, s *store.Store) error {
		buckets, err := s.ListBuckets()
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, b := range knownSectionOrder {
			known[b] = true
		}
		sections := append([]string{}, knownSectionOrder...)
		for _, b := range buckets {
			if !known[b] {
				sections = append(sections, b)
			}
		}
		out := cmd.OutOrStdout()
		first := true
		for _, b := range sections {
			entries, err := s.List(b)
			if err != nil {
				return err
			}
			if !known[b] && len(entries) == 0 {
				continue
			}
			if !first {
				fmt.Fprintln(out)
			}
			first = false
			fmt.Fprintf(out, "[%s]\n", b)
			for _, kv := range entries {
				value := kv.Value
				if b == store.BucketProject && kv.Key == "api_key" {
					value = "****"
				}
				fmt.Fprintf(out, "  %s = %s\n", displayKey(b, kv.Key), value)
			}
		}
		return nil
	})
}
