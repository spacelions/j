//go:build !windows

package run

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacelions/j/internal/util/agentlog"
)

const pipeWaitTimeout = 5 * time.Second

func waitForLogPipe(t *testing.T, logPath, want string) string {
	t.Helper()
	deadline := time.Now().Add(pipeWaitTimeout)
	for {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), want) {
			return string(data)
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"timeout waiting for %q in %s; last=%q err=%v",
				want, logPath, data, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSpawnPipedIn_FormatsStdoutAndStderr drives a stub binary that
// emits one Claude-shaped JSON event on stdout and a plain line on
// stderr; the formatter should turn them into `text:` and
// `stderr:` log lines, both written before the `child exit` marker.
func TestSpawnPipedIn_FormatsStdoutAndStderr(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	script := `printf '{"type":"assistant","message":` +
		`{"content":[{"type":"text","text":"hi"}]}}\n';` +
		`echo "kaboom" >&2`
	pid, err := SpawnPipedIn(
		t.Context(), "", logPath,
		agentlog.ClaudeStream(),
		"sh", "-c", script,
	)
	if err != nil {
		t.Fatalf("SpawnPipedIn: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	body := waitForLogPipe(t, logPath, "child exit")
	for _, want := range []string{
		"text: hi", "stderr: kaboom",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in log:\n%s", want, body)
		}
	}
	// Ordering: each formatter line must appear before the child
	// exit marker so a tailer never sees the marker without the
	// preceding stream content.
	for _, want := range []string{"text: hi", "stderr: kaboom"} {
		idxOut := strings.Index(body, want)
		idxExit := strings.Index(body, "child exit")
		if idxOut < 0 || idxExit < 0 || idxOut > idxExit {
			t.Fatalf(
				"ordering: %q at %d, child exit at %d",
				want, idxOut, idxExit)
		}
	}
}

// TestSpawnPipedIn_NonZeroExit pins that formatter output is still
// flushed when the child exits non-zero — the marker carries
// exit_code=N afterwards.
func TestSpawnPipedIn_NonZeroExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := SpawnPipedIn(
		t.Context(), "", logPath,
		agentlog.PassThrough(),
		"sh", "-c", "echo done; exit 7",
	)
	if err != nil {
		t.Fatalf("SpawnPipedIn: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	body := waitForLogPipe(t, logPath, "exit_code=7")
	if !strings.Contains(body, "done") {
		t.Fatalf("missing stdout: %s", body)
	}
}

// TestSpawnPiped_WorkspacelessVariant exercises the
// workspace-less wrapper used by cursor (which threads its own
// --workspace flag through argv).
func TestSpawnPiped_WorkspacelessVariant(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := SpawnPiped(
		t.Context(), logPath,
		agentlog.PassThrough(),
		"sh", "-c", "echo flat",
	)
	if err != nil {
		t.Fatalf("SpawnPiped: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	body := waitForLogPipe(t, logPath, "child exit")
	if !strings.Contains(body, "flat") {
		t.Fatalf("missing stdout: %s", body)
	}
}

// TestSpawnPipedIn_EmptyLogPath pins the empty-log-path guard,
// mirroring TestSpawn_EmptyLogPath.
func TestSpawnPipedIn_EmptyLogPath(t *testing.T) {
	t.Parallel()
	_, err := SpawnPipedIn(
		t.Context(), "", "",
		agentlog.PassThrough(), "true",
	)
	if err == nil ||
		!strings.Contains(err.Error(), "empty log path") {
		t.Fatalf("err = %v", err)
	}
}

// TestSpawnPipedIn_LogOpenFails covers the "log path is a
// directory" branch: OpenFile rejects the path so SpawnPipedIn
// returns before fork.
func TestSpawnPipedIn_LogOpenFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := SpawnPipedIn(
		t.Context(), "", dir,
		agentlog.PassThrough(), "true",
	)
	if err == nil {
		t.Fatal("expected open error when logPath is a directory")
	}
}

// TestSpawnPipedIn_MissingBinary covers the cmd.Start error path.
// A non-existent binary fails fork/exec; the wrapped error names
// the requested binary and the child pipes are still closed.
func TestSpawnPipedIn_MissingBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	_, err := SpawnPipedIn(
		t.Context(), "", logPath,
		agentlog.PassThrough(),
		"/no/such/binary-zzzzz",
	)
	if err == nil {
		t.Fatal("expected error from missing binary")
	}
	if !strings.Contains(err.Error(), "binary-zzzzz") {
		t.Fatalf("err = %v", err)
	}
}

// TestSpawnPipedIn_PlumbsCwd asserts the dir argument is honoured
// (mirrors TestSpawnIn_PlumbsCwd).
func TestSpawnPipedIn_PlumbsCwd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	_, err := SpawnPipedIn(
		t.Context(), dir, logPath,
		agentlog.PassThrough(),
		"sh", "-c", "pwd",
	)
	if err != nil {
		t.Fatalf("SpawnPipedIn: %v", err)
	}
	want, evalErr := filepath.EvalSymlinks(dir)
	if evalErr != nil {
		want = dir
	}
	deadline := time.Now().Add(pipeWaitTimeout)
	for {
		body, _ := os.ReadFile(logPath)
		for line := range strings.SplitSeq(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || markerLine.MatchString(line) {
				continue
			}
			got, evalErr := filepath.EvalSymlinks(line)
			if evalErr != nil {
				got = line
			}
			if got == want {
				return
			}
			t.Fatalf("cwd = %q, want %q", got, want)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: log = %q", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSpawnPipedIn_LongLineNoTruncation drives a child that emits
// a single 300 KiB JSON event on one line; the formatter must emit
// it without splitting / truncation. Uses a deliberately oversized
// payload past the bufio default to confirm we use a sized reader.
func TestSpawnPipedIn_LongLineNoTruncation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	const n = 300_000
	bigText := strings.Repeat("z", n)
	script := `printf '{"type":"assistant","message":` +
		`{"content":[{"type":"text","text":"` +
		bigText + `"}]}}\n'`
	_, err := SpawnPipedIn(
		t.Context(), "", logPath,
		agentlog.ClaudeStream(),
		"sh", "-c", script,
	)
	if err != nil {
		t.Fatalf("SpawnPipedIn: %v", err)
	}
	body := waitForLogPipe(t, logPath, "text: ")
	zCount := strings.Count(body, "z")
	if zCount < n {
		t.Fatalf("got %d z's, want >= %d", zCount, n)
	}
}

func TestSpawnPipedIn_StableContextWiring(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	ctx, cancel := context.WithTimeout(
		t.Context(), 2*time.Second)
	defer cancel()
	pid, err := SpawnPipedIn(
		ctx, "", logPath,
		agentlog.PassThrough(),
		"sh", "-c", "echo wired; sleep 0.05",
	)
	if err != nil {
		t.Fatalf("SpawnPipedIn: %v", err)
	}
	if err := WaitForExit(ctx, pid); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	body := waitForLogPipe(t, logPath, "wired")
	if !strings.Contains(body, "child exit") {
		t.Fatalf("missing child exit marker: %s", body)
	}
}
