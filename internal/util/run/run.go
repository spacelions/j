// Package run provides a small abstraction over os/exec for running
// short-lived external commands. Output captures stdout (headless use);
// Run inherits the parent's stdin/stdout/stderr (interactive TUIs).
// A single fake implementation in tests is enough to exercise every
// caller.
package run

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner runs short-lived external commands. The default implementation
// shells out via os/exec; tests substitute a scripted fake.
type Runner interface {
	// Output runs the command and returns its captured stdout. Use this
	// for headless calls that need to parse the result.
	Output(ctx context.Context, name string, args ...string) (string, error)
	// Run executes the command with stdin/stdout/stderr wired to the
	// parent process so an interactive TUI can render. It blocks until
	// the child exits.
	Run(ctx context.Context, name string, args ...string) error
}

// NewExec returns a Runner that shells out to real binaries.
func NewExec() Runner { return execRunner{} }

type execRunner struct{}

func (execRunner) Output(ctx context.Context, name string, args ...string) (string, error) {
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

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
