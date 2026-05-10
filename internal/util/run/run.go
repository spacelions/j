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
	"syscall"
	"time"
)

// TerminateGrace is the wall-clock window Run / RunIn give a phase
// child to react to a forwarded SIGTERM (via exec.Cmd.Cancel) before
// exec escalates to SIGKILL via cmd.WaitDelay. Two seconds matches
// Terminate's grace and is short enough that an interactive Ctrl+C
// feels snappy while still letting a coding-agent flush the open
// agent.log fd.
const TerminateGrace = 2 * time.Second

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
	return RunIn(ctx, "", name, args...)
}

// RunIn is Run with an explicit working directory. Used by backends
// whose CLI has no `--workspace`-style flag (e.g. claude) so the
// workspace concept maps onto the child's CWD via cmd.Dir. An empty
// dir inherits the parent's CWD, matching exec.Cmd's default.
//
// On ctx cancellation the child receives SIGTERM (via cmd.Cancel) and
// gets TerminateGrace to exit cleanly before exec escalates to
// SIGKILL via cmd.WaitDelay. This is the cascade the orchestrator's
// SIGTERM signal handler relies on so the per-task flock truly
// releases on shutdown.
func RunIn(ctx context.Context, dir, name string, args ...string) error {
	return RunInEnv(ctx, dir, nil, name, args...)
}

// RunInEnv is RunIn with caller-supplied environment overrides. Codex
// and deepseek use it to point each child at a task-scoped agent home.
// The supplied env entries are appended after os.Environ(), so later
// duplicate keys win under os/exec's environment handling.
func RunInEnv(
	ctx context.Context, dir string, env []string,
	name string, args ...string,
) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = mergedEnv(env)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = TerminateGrace
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func mergedEnv(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(os.Environ())+len(env))
	out = append(out, os.Environ()...)
	out = append(out, env...)
	return out
}
