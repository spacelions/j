package run

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// SpawnFormattedIn is SpawnIn with a per-line formatter applied to the
// child's stdout/stderr stream before the bytes hit logPath. lineFmt
// is called once per `\n`-terminated line off a single drain goroutine
// so callers do not need their own synchronisation; the returned bytes
// (with whatever newline terminators the formatter chose) are written
// straight to the open log file. Each line is read via bufio.Reader's
// ReadBytes('\n') so there is no token-size cap — cursor's tool_result
// envelopes can carry many KB of file content per line.
//
// The reap goroutine waits for the drain goroutine to finish flushing
// before emitting the `child_exit` marker, so the marker always lands
// after the last formatted line of child output. The drain goroutine
// sees EOF on the read end once Wait reaps the child (the parent's
// copy of the pipe write end is closed inside startFormatted).
//
// A nil lineFmt is treated as identity (line passes through unchanged)
// so callers that need the SpawnIn behaviour but want to hand a
// nil-able value do not have to branch.
func SpawnFormattedIn(
	ctx context.Context, dir, logPath string,
	lineFmt func([]byte) []byte,
	name string, args ...string,
) (int, error) {
	return SpawnFormattedInEnv(
		ctx, dir, nil, logPath, lineFmt, name, args...,
	)
}

// SpawnFormattedInEnv is SpawnFormattedIn with caller-supplied
// environment overrides. The supplied env entries are appended after
// os.Environ(), so later duplicate keys win under os/exec's
// environment handling.
func SpawnFormattedInEnv(
	ctx context.Context, dir string, env []string, logPath string,
	lineFmt func([]byte) []byte,
	name string, args ...string,
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
	pr, pw, err := os.Pipe()
	if err != nil {
		_ = logFile.Close()
		return 0, fmt.Errorf("run: pipe: %w", err)
	}
	pid, err := startFormatted(
		ctx, dir, env, name, args, devNull, pw,
		logFile, lineFmt, pr,
	)
	if err != nil {
		_ = pr.Close()
		_ = pw.Close()
		_ = logFile.Close()
		return 0, err
	}
	return pid, nil
}

// startFormatted launches the child wired to devNull/pw, kicks off the
// drain + reap goroutines, and closes the parent's pw copy so the
// drain side will see EOF after the child exits. Split out of
// SpawnFormattedIn to keep that helper under the 80-line method cap.
func startFormatted(
	ctx context.Context, dir string, env []string,
	name string, args []string,
	devNull, pw, logFile *os.File,
	lineFmt func([]byte) []byte, pr *os.File,
) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = mergedEnv(env)
	cmd.Stdin = devNull
	cmd.Stdout = pw
	cmd.Stderr = pw
	applyDetachAttrs(cmd)
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	pid := cmd.Process.Pid
	_ = pw.Close()
	drainDone := make(chan struct{})
	go drainFormatted(pr, logFile, lineFmt, drainDone)
	go reapFormatted(cmd, logFile, name, pid, started, drainDone)
	return pid, nil
}

// reapFormatted waits for the child, then for the drain goroutine's
// final flush, and finally appends the child_exit marker so it always
// lands after the last formatted line of child output.
func reapFormatted(
	cmd *exec.Cmd, logFile *os.File,
	name string, pid int, started time.Time,
	drainDone <-chan struct{},
) {
	_ = cmd.Wait()
	<-drainDone
	emitChildExit(logFile, name, pid, cmd.ProcessState, started)
	_ = logFile.Close()
}

// drainFormatted reads pr line-by-line and writes each formatted line
// to logFile. A nil lineFmt is treated as identity. Closes pr and
// signals drainDone when the read end returns EOF.
func drainFormatted(
	pr, logFile *os.File,
	lineFmt func([]byte) []byte, drainDone chan<- struct{},
) {
	defer close(drainDone)
	defer func() { _ = pr.Close() }()
	br := bufio.NewReader(pr)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			out := line
			if lineFmt != nil {
				out = lineFmt(line)
			}
			if len(out) > 0 {
				_, _ = logFile.Write(out)
			}
		}
		if err != nil {
			return
		}
	}
}
