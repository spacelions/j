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
	path := store.DefaultPath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			uitheme.NormalFprintln(cmd.OutOrStdout(), "J: no settings stored")
			return nil
		}
		return err
	}

	return withOpenStore(func(_ string, s *store.Store) error {
		sections, known, _ := collectSections(s)
		return printSections(cmd.OutOrStdout(), sections, known, s)
	})
}

// collectSections merges the fixed display order with any extra buckets
// that exist in the store. known marks buckets with a fixed position so
// empty unknown buckets can be skipped.
func collectSections(
	s *store.Store,
) (sections []string, known map[string]bool, err error) {
	buckets, err := s.ListBuckets()
	if err != nil {
		return nil, nil, err
	}
	known = make(map[string]bool, len(knownSectionOrder))
	for _, b := range knownSectionOrder {
		known[b] = true
	}
	sections = append(sections, knownSectionOrder...)
	for _, b := range buckets {
		if !known[b] {
			sections = append(sections, b)
		}
	}
	return sections, known, nil
}

// printSections renders each section header and its key=value entries.
type byteWriter interface{ Write([]byte) (int, error) }

func printSections(
	out byteWriter, sections []string, known map[string]bool, s *store.Store,
) error {
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
			if isSecretKey(b, kv.Key) {
				value = "****"
			}
			fmt.Fprintf(out, "  %s = %s\n", displayKey(b, kv.Key), value)
		}
	}
	return nil
}
