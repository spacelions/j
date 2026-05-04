package settings

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/store"
)

func newResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset [bucket|bucket.key ...]",
		Short: "Remove all project settings, a whole bucket, or one bucket.key (multiple targets allowed)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReset(cmd, args)
		},
	}
	cmd.Flags().BoolP("yes", "y", false, "skip the confirmation prompt for a full reset")
	return cmd
}

func runReset(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return runResetFull(cmd)
	}
	return runResetTargets(cmd, args)
}

func runResetFull(cmd *cobra.Command) error {
	jDir, err := store.DefaultDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(jDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			banner.Fprintln(cmd.OutOrStdout(), "J: nothing to reset")
			return nil
		}
		return err
	}
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			banner.Fprintln(cmd.OutOrStdout(), "J: nothing to reset")
			return nil
		}
		return err
	}
	s, err := store.Open(path)
	if err != nil {
		return err
	}
	empty, err := s.IsEmpty()
	if err != nil {
		_ = s.Close()
		return err
	}
	if err := s.Close(); err != nil {
		return err
	}
	if empty {
		banner.Fprintln(cmd.OutOrStdout(), "J: nothing to reset")
		return nil
	}
	skip, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return err
	}
	if !skip {
		answer, err := readConfirmationLine(cmd)
		if err != nil {
			return err
		}
		if !isYesAnswer(answer) {
			banner.DangerousFprintln(cmd.OutOrStdout(), "J: reset aborted")
			return nil
		}
	}
	if err := os.RemoveAll(jDir); err != nil {
		return err
	}
	banner.DangerousFprintf(cmd.OutOrStdout(), "J: removed %s\n", jDir)
	return nil
}

func readConfirmationLine(cmd *cobra.Command) (string, error) {
	r := bufio.NewReader(cmd.InOrStdin())
	line, err := r.ReadString('\n')
	trim := strings.TrimSpace(line)
	if err != nil {
		if errors.Is(err, io.EOF) && line == "" {
			return "", nil
		}
		if !errors.Is(err, io.EOF) {
			return "", err
		}
	}
	return trim, nil
}

func isYesAnswer(s string) bool {
	lower := strings.ToLower(s)
	return lower == "y" || lower == "yes"
}

// resetTarget is one parsed positional arg: either a whole bucket
// (isBucket==true, key==""), or a single bucket.key entry.
type resetTarget struct {
	bucket   string
	key      string
	isBucket bool
}

// parseResetTargets validates each positional arg and routes it to
// either a bucket-level or bucket.key target. Whitespace is the only
// argument separator (cobra has already split on it); literal commas
// or semicolons inside an arg are part of the bucket/key name and
// only fail when they leave bucket or key empty (handled by
// parseBucketKey).
func parseResetTargets(args []string) ([]resetTarget, error) {
	out := make([]resetTarget, 0, len(args))
	for _, arg := range args {
		if arg == "" {
			return nil, fmt.Errorf("settings: empty reset target")
		}
		if strings.ContainsRune(arg, '.') {
			bucket, key, err := parseBucketKey(arg)
			if err != nil {
				return nil, err
			}
			out = append(out, resetTarget{bucket: bucket, key: key})
			continue
		}
		out = append(out, resetTarget{bucket: arg, isBucket: true})
	}
	return out, nil
}

func runResetTargets(cmd *cobra.Command, args []string) error {
	targets, err := parseResetTargets(args)
	if err != nil {
		return err
	}
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			banner.Fprintln(cmd.OutOrStdout(), "J: nothing to reset")
			return nil
		}
		return err
	}
	return withOpenStore(func(_ string, s *store.Store) error {
		for _, t := range targets {
			if t.isBucket {
				if err := s.DeleteBucket(t.bucket); err != nil {
					return err
				}
				banner.DangerousFprintf(cmd.OutOrStdout(), "J: unset %s\n", t.bucket)
				continue
			}
			if err := s.Delete(t.bucket, storageKey(t.bucket, t.key)); err != nil {
				return err
			}
			banner.DangerousFprintf(cmd.OutOrStdout(), "J: unset %s.%s\n", t.bucket, t.key)
		}
		return nil
	})
}
