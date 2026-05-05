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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/spacelions/j/internal/util/agentlog"
)

// waitForExitPollInterval is the polling cadence WaitForExit uses to
// re-check IsAlive. 100ms is fast enough that the verify orchestrator
// barely notices the wait when a real cursor-agent / claude turn
// finishes (tens of seconds), and slow enough not to burn CPU on a
// tight loop while the child is mid-write.
const waitForExitPollInterval = 100 * time.Millisecond

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
func RunIn(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Spawn launches name with args as a detached background process and
// returns the spawned child's PID. The child's stdout and stderr are
// redirected to logPath (created or appended, mode 0o644) and stdin
// is wired to /dev/null so the child cannot block on tty input. On
// POSIX the child is given a fresh session via setsid (see
// run_posix.go) so it survives SIGHUP / terminal close like nohup.
//
// Append (rather than truncate) is the documented contract: a single
// per-task `agent.log` is shared by every phase (planner, worker,
// verifier — and every retry iteration of the worker→verifier loop)
// and across the orchestrator that drives them, so earlier bytes
// must survive later spawns.
//
// The returned PID is the OS process id at Start time. Spawn does not
// expose a Wait handle — callers treat the child as fire-and-forget by
// PID and rely on a later reaper (or WaitForExit) that polls IsAlive.
// A background goroutine calls cmd.Wait so the kernel reaps the child
// promptly when it exits; without that reap the child lingers as a
// zombie and signal(0)-based IsAlive reports it alive forever, which
// would deadlock WaitForExit. The goroutine has no other side effects:
// it does not bind the child to ctx and does not signal it. In the
// fire-and-forget case the parent exits before the goroutine can run
// and the OS re-parents the orphan to init, which then reaps it.
// SpawnIn hands the open `agent.log` file descriptor to that goroutine
// (rather than reopening by path after Wait) and the goroutine closes
// it once the `child_exit` marker is appended. Holding the fd across
// Wait means a concurrent `os.RemoveAll` of the task directory unlinks
// the file but the post-exit write still goes through the open
// descriptor — no new directory entry is created underneath RemoveAll,
// which would otherwise resurrect the file and cause the parent
// `rmdir` to fail with "directory not empty".
//
// ctx is consulted only by exec.CommandContext for the brief window
// before Start returns; once the child has been started Spawn does
// not bind its lifetime to ctx (a fire-and-forget child cannot be
// safely killed by the parent's context cancellation without
// re-introducing a Wait dependency, which is the very seam this
// helper avoids).
func Spawn(ctx context.Context, logPath, name string, args ...string) (int, error) {
	return SpawnIn(ctx, "", logPath, name, args...)
}

// SpawnIn is Spawn with an explicit working directory. Used by backends
// whose CLI has no `--workspace`-style flag (e.g. claude) so the
// workspace concept maps onto the child's CWD via cmd.Dir. An empty
// dir inherits the parent's CWD, matching exec.Cmd's default.
func SpawnIn(ctx context.Context, dir, logPath, name string, args ...string) (int, error) {
	if logPath == "" {
		return 0, errors.New("run: empty log path")
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("run: open log %q: %w", logPath, err)
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		_ = logFile.Close()
		return 0, fmt.Errorf("run: open /dev/null: %w", err)
	}
	defer func() { _ = devNull.Close() }()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = devNull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	applyDetachAttrs(cmd)
	started := time.Now()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	pid := cmd.Process.Pid
	// Reap the child via cmd.Wait in a goroutine instead of detaching
	// with Process.Release. Release just drops Go's process bookkeeping
	// and never calls wait4, which leaves the exited child as a zombie;
	// signal(0) on a zombie returns success on Linux/macOS, so IsAlive
	// reports it alive and WaitForExit polls forever. Calling Wait here
	// has no effect on the child's lifecycle (no signal, no ctx binding)
	// — it just lets the kernel drop the zombie so IsAlive flips to
	// false promptly. In the fire-and-forget path the parent exits
	// before this goroutine runs and the orphan is reparented to init,
	// which reaps it exactly as before. After Wait, append a
	// `child_exit` marker to the same agent.log so a tailer sees the
	// child's exit code without opening bbolt.
	go func() {
		_ = cmd.Wait()
		emitChildExit(logFile, name, pid, cmd.ProcessState, started)
		_ = logFile.Close()
	}()
	return pid, nil
}

// emitChildExit appends one `child_exit` marker line through w (the
// open `agent.log` handle owned by the SpawnIn reap goroutine).
// Errors are intentionally swallowed: the child has already exited
// and the parent is already reaping; a missing marker is strictly
// less harmful than a noisy logger goroutine.
func emitChildExit(w io.Writer, name string, pid int, state *os.ProcessState, started time.Time) {
	fields := map[string]any{
		"name":        name,
		"pid":         pid,
		"duration_ms": time.Since(started).Milliseconds(),
	}
	if state != nil {
		fields["exit_code"] = state.ExitCode()
	}
	_ = agentlog.Emit(w, "child_exit", fields)
}

// WaitForExit blocks until the OS process identified by pid is no
// longer alive, returning nil. It is intended for verify-loop
// synchronisation against Spawn-ed children: Spawn does not expose a
// Wait handle to the caller (a background goroutine inside Spawn does
// the wait4 reap so the kernel does not keep zombies around), so the
// orchestrator polls IsAlive instead and reads the child's findings
// file only after this returns. WaitForExit polls IsAlive on a small
// ticker and returns ctx.Err() if the context is cancelled before
// the child exits.
//
// pid <= 0 returns nil immediately because Spawn reserves 0 for the
// "synchronous / nothing to wait on" case and negative ids are
// illegal — neither calls for a wait. An already-dead pid also
// returns nil immediately on the first IsAlive check.
func WaitForExit(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	if !IsAlive(pid) {
		return nil
	}
	ticker := time.NewTicker(waitForExitPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !IsAlive(pid) {
				return nil
			}
		}
	}
}

// IsAlive reports whether the OS process identified by pid is still
// running. It uses os.FindProcess + signal 0 (the standard "no-op"
// liveness probe on POSIX): an ESRCH / os.ErrProcessDone error means
// the process is gone. Any other error is conservatively treated as
// "alive" so a transient permission error does not cause the reaper
// to declare a still-running child dead.
//
// IsAlive returns false for pid <= 0 because the spawn helpers reserve
// 0 for "synchronous / nothing to reap" and negative ids are illegal.
func IsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	// EPERM means the process exists but is owned by another user
	// (or we are not allowed to signal it). Treat as alive.
	return true
}
