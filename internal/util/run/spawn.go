package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/spacelions/j/internal/util/agentlog"
)

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
// and the goroutine closes it once the `child_exit` marker is appended.
//
// ctx is consulted only by exec.CommandContext for the brief window
// before Start returns; once the child has been started Spawn does
// not bind its lifetime to ctx.
func Spawn(
	ctx context.Context, logPath, name string, args ...string,
) (int, error) {
	return SpawnIn(ctx, "", logPath, name, args...)
}

// SpawnIn is Spawn with an explicit working directory. Used by backends
// whose CLI has no `--workspace`-style flag (e.g. claude) so the
// workspace concept maps onto the child's CWD via cmd.Dir. An empty
// dir inherits the parent's CWD, matching exec.Cmd's default.
func SpawnIn(
	ctx context.Context, dir, logPath, name string, args ...string,
) (int, error) {
	if logPath == "" {
		return 0, errors.New("run: empty log path")
	}
	logFile, err := os.OpenFile(
		logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
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
func emitChildExit(
	w io.Writer, name string, pid int,
	state *os.ProcessState, started time.Time,
) {
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

// applyDetachAttrs configures cmd so the spawned child runs in a
// fresh POSIX session (setsid). Detaching from the parent's
// controlling terminal lets the child survive SIGHUP / terminal
// close, which is the equivalent of `nohup` for fire-and-forget
// background runs of cursor-agent.
func applyDetachAttrs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
