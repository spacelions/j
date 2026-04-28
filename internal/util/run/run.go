// Package run provides a small abstraction over os/exec for running
// short-lived external commands and capturing their stdout. It is shared
// across the codebase (coding-agent backends, future helpers, ...) so a
// single fake implementation in tests is enough to exercise every caller.
package run

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs short-lived external commands. The default implementation
// shells out via os/exec; tests substitute a scripted fake.
type Runner interface {
	Output(ctx context.Context, name string, args ...string) (string, error)
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
