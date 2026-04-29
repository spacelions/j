package settings

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/store"
)

func newSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [bucket.key] [value]",
		Short: "Set a value under bucket.key in the local settings database",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			return runSet(c, args)
		},
	}
}

func runSet(cmd *cobra.Command, args []string) error {
	bucket, key, err := parseBucketKey(args[0])
	if err != nil {
		return err
	}
	return withOpenStore(func(_ string, s *store.Store) error {
		if err := s.EnsureBucket(bucket); err != nil {
			return err
		}
		if err := s.Put(bucket, key, args[1]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "set %s.%s = %s\n", bucket, key, args[1])
		return nil
	})
}

func parseBucketKey(s string) (bucket, key string, err error) {
	i := strings.IndexByte(s, '.')
	if i < 0 {
		return "", "", fmt.Errorf("settings: %q is not a valid bucket.key (missing '.')", s)
	}
	bucket, key = s[:i], s[i+1:]
	if bucket == "" {
		return "", "", fmt.Errorf("settings: bucket name must be non-empty in %q", s)
	}
	if key == "" {
		return "", "", fmt.Errorf("settings: key must be non-empty in %q", s)
	}
	return bucket, key, nil
}
