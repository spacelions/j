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

	"github.com/spacelions/j/internal/store"
)

func newResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset [bucket.key]",
		Short: "Remove all project settings, or a single bucket.key",
		Args:  cobra.RangeArgs(0, 1),
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
	return runResetOneKey(cmd, args[0])
}

func runResetFull(cmd *cobra.Command) error {
	jDir, err := store.DefaultDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(jDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(cmd.OutOrStdout(), "nothing to reset")
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
			fmt.Fprintln(cmd.OutOrStdout(), "nothing to reset")
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
		fmt.Fprintln(cmd.OutOrStdout(), "nothing to reset")
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
			fmt.Fprintln(cmd.OutOrStdout(), "reset aborted")
			return nil
		}
	}
	if err := os.RemoveAll(jDir); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", jDir)
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

func runResetOneKey(cmd *cobra.Command, arg string) error {
	bucket, key, err := parseBucketKey(arg)
	if err != nil {
		return err
	}
	path, err := store.DefaultPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintln(cmd.OutOrStdout(), "nothing to reset")
			return nil
		}
		return err
	}
	return withOpenStore(func(_ string, s *store.Store) error {
		if err := s.Delete(bucket, key); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "unset %s.%s\n", bucket, key)
		return nil
	})
}
