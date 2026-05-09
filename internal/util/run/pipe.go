package run

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/spacelions/j/internal/util/agentlog"
)

// SpawnPiped is the workspace-less variant of SpawnPipedIn for
// backends (cursor) whose CLI accepts `--workspace` so cmd.Dir is
// irrelevant. It mirrors run.Spawn / run.SpawnIn.
func SpawnPiped(
	ctx context.Context, logPath string,
	fmtr agentlog.LineFormatter, name string, args ...string,
) (int, error) {
	return SpawnPipedIn(ctx, "", logPath, fmtr, name, args...)
}

// SpawnPipedIn is the formatter-aware sibling of SpawnIn: instead
// of pointing the child's stdout / stderr at the log file directly,
// it builds an os.Pipe() pair per stream, runs each scanned line
// through fmtr, and writes the formatter's output through
// agentlog.WriteLines (which holds emitMu) so backend output cannot
// interleave with the lifecycle marker writes that share the same
// log.
//
// Ordering contract: every byte the child writes to either pipe is
// flushed through fmtr and into the log before the `child_exit`
// marker is appended. The reaper goroutine waits for both reader
// goroutines via a sync.WaitGroup before emitting the marker.
//
// Other than the formatter routing, the function preserves
// SpawnIn's guarantees: stdin is /dev/null, the child gets a fresh
// POSIX session via applyDetachAttrs (so SIGHUP / terminal close
// does not reach it), the kernel reaps via cmd.Wait inside a
// goroutine, and the open log fd survives across an `os.RemoveAll`
// of the task dir.
func SpawnPipedIn(
	ctx context.Context, dir, logPath string,
	fmtr agentlog.LineFormatter,
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

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = logFile.Close()
		return 0, fmt.Errorf("run: pipe stdout: %w", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = logFile.Close()
		return 0, fmt.Errorf("run: pipe stderr: %w", err)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = devNull
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	applyDetachAttrs(cmd)
	started := time.Now()
	if err := cmd.Start(); err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
		_ = logFile.Close()
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	pid := cmd.Process.Pid
	// The child has its own fd duplicates after fork/exec; close
	// the parent's write ends so the read ends see EOF when the
	// child exits.
	_ = stdoutW.Close()
	_ = stderrW.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go drainPipe(stdoutR, fmtr.Format, logFile, &wg)
	go drainPipe(stderrR, fmtr.FormatStderr, logFile, &wg)

	go func() {
		_ = cmd.Wait()
		wg.Wait()
		_ = stdoutR.Close()
		_ = stderrR.Close()
		emitChildExit(
			logFile, name, pid, cmd.ProcessState, started)
		_ = logFile.Close()
	}()
	return pid, nil
}

// drainPipe reads r line by line and feeds each line to fmt,
// writing the formatter's output through agentlog.WriteLines. Uses
// a bufio.Reader (not a Scanner) so a single oversized JSON event
// does not abort the reader with bufio.ErrTooLong — the formatter's
// caller already decides what to do with very long lines.
func drainPipe(
	r io.Reader, fmt func([]byte) [][]byte,
	w io.Writer, wg *sync.WaitGroup,
) {
	defer wg.Done()
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if out := fmt(line); len(out) > 0 {
				_ = agentlog.WriteLines(w, out)
			}
		}
		if err != nil {
			return
		}
	}
}
