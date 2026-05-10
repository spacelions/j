package settings

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store"
)

func newSetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "set <bucket.key=value> [bucket.key=value ...]",
		Short: "Set one or more values under bucket.key " +
			"in the local settings database",
		Args: cobra.MinimumNArgs(1),
		RunE: runSet,
	}
}

type setEntry struct {
	bucket, key, storedKey, value string
}

func runSet(cmd *cobra.Command, args []string) error {
	entries := make([]setEntry, 0, len(args))
	for _, arg := range args {
		bucket, key, value, err := parseKeyValue(arg)
		if err != nil {
			return err
		}
		storedKey := storageKey(bucket, key)
		entries = append(entries, setEntry{
			bucket:    bucket,
			key:       key,
			storedKey: storedKey,
			value:     value,
		})
	}
	return withOpenStore(func(_ string, s *store.Store) error {
		out := cmd.OutOrStdout()
		for _, e := range entries {
			if err := maybeSeedPromptFile(out, e); err != nil {
				return err
			}
			if err := s.EnsureBucket(e.bucket); err != nil {
				return err
			}
			if err := s.Put(e.bucket, e.storedKey, e.value); err != nil {
				return err
			}
			uitheme.NormalFprintf(out, "J: set %s.%s = %s\n", e.bucket, e.key, e.value)
		}
		return nil
	})
}

// parseKeyValue splits arg on the first '=' so values may contain
// additional '=' characters (e.g. "foo.bar=a=b" stores literal "a=b").
// The bucket.key portion is delegated to parseBucketKey so the
// reset path keeps using the same validation.
func parseKeyValue(arg string) (bucket, key, value string, err error) {
	before, after, ok := strings.Cut(arg, "=")
	if !ok {
		return "", "", "", fmt.Errorf(
			"settings: %q is not a valid key=value (missing '=')",
			arg,
		)
	}
	bucket, key, err = parseBucketKey(before)
	if err != nil {
		return "", "", "", err
	}
	return bucket, key, after, nil
}

// parseBucketKey splits a `bucket.key` argument into its parts. The
// returned key is the user-typed display form (kebab); callers
// translate to the bbolt storage form via storageKey before hitting
// *store.Store so the on-disk row uses the canonical camelCase name
// while the echo-back / error wording keeps the form the user typed.
func parseBucketKey(s string) (bucket, key string, err error) {
	before, after, ok := strings.Cut(s, ".")
	if !ok {
		return "", "", fmt.Errorf(
			"settings: %q is not a valid bucket.key (missing '.')",
			s,
		)
	}
	bucket, key = before, after
	if bucket == "" {
		return "", "", fmt.Errorf("settings: bucket name must be non-empty in %q", s)
	}
	if key == "" {
		return "", "", fmt.Errorf("settings: key must be non-empty in %q", s)
	}
	return bucket, key, nil
}
