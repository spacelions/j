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

type formattedPipes struct {
	DevNull   *os.File
	PipeWrite *os.File
	PipeRead  *os.File
	LogFile   *os.File
}

type formattedRun struct {
	Cmd       *exec.Cmd
	Name      string
	PID       int
	Started   time.Time
	LogFile   *os.File
	DrainDone chan struct{}
}

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
	// /dev/null is always present and os.Pipe rarely fails on POSIX.
	devNull, _ := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	defer func() { _ = devNull.Close() }()
	pr, pw, _ := os.Pipe()
	pid, err := startFormatted(ctx, dir, env, name, args, formattedPipes{
		DevNull:   devNull,
		PipeWrite: pw,
		PipeRead:  pr,
		LogFile:   logFile,
	}, lineFmt)
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
	pipes formattedPipes, lineFmt func([]byte) []byte,
) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = mergedEnv(env)
	cmd.Stdin = pipes.DevNull
	cmd.Stdout = pipes.PipeWrite
	cmd.Stderr = pipes.PipeWrite
	applyDetachAttrs(cmd)
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	pid := cmd.Process.Pid
	_ = pipes.PipeWrite.Close()
	drainDone := make(chan struct{})
	run := &formattedRun{
		Cmd:       cmd,
		Name:      name,
		PID:       pid,
		Started:   started,
		LogFile:   pipes.LogFile,
		DrainDone: drainDone,
	}
	go drainFormatted(run, pipes.PipeRead, lineFmt)
	go reapFormatted(run)
	return pid, nil
}

// reapFormatted waits for the child, then for the drain goroutine's
// final flush, and finally appends the child_exit marker so it always
// lands after the last formatted line of child output.
func reapFormatted(run *formattedRun) {
	_ = run.Cmd.Wait()
	<-run.DrainDone
	emitChildExit(
		run.LogFile, run.Name, run.PID,
		run.Cmd.ProcessState, run.Started,
	)
	_ = run.LogFile.Close()
}

// drainFormatted reads pr line-by-line and writes each formatted line
// to logFile. A nil lineFmt is treated as identity. Closes pr and
// signals drainDone when the read end returns EOF.
func drainFormatted(
	run *formattedRun, pr *os.File, lineFmt func([]byte) []byte,
) {
	defer close(run.DrainDone)
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
				_, _ = run.LogFile.Write(out)
			}
		}
		if err != nil {
			return
		}
	}
}
