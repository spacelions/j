// Package run provides two thin helpers around os/exec for running
// short-lived external commands. Output captures stdout (headless use);
// Run inherits the parent's stdin/stdout/stderr (interactive TUIs).
//
// The package intentionally exposes plain functions rather than an
// interface: per AGENTS.md, callers shell out to real binaries and tests
// drive them via PATH-resolvable stub executables instead of an
// injection seam.
package run

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Output runs name with args and returns its captured stdout. The wrapped
// error includes name plus stderr (or stdout if stderr is empty) so the
// caller can surface a useful message without re-reading the streams.
func Output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return "", fmt.Errorf("%s: %w: %s", name, err, msg)
	}
	return stdout.String(), nil
}

// Run executes name with args, wiring stdin/stdout/stderr to the parent
// process so an interactive TUI can render. It blocks until the child
// exits and wraps the exit error with name for context.
func Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
