package run

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSpawnFormattedIn_AppliesFormatterPerLine pins the per-line
// transform contract: lineFmt is invoked once per `\n`-terminated
// child output line and the returned bytes are written to logPath
// in order, before the trailing `child exit` marker.
func TestSpawnFormattedIn_AppliesFormatterPerLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := SpawnFormattedIn(
		t.Context(), "", logPath, bytes.ToUpper, "sh", "-c",
		"printf 'hello\\nworld\\n'")
	if err != nil {
		t.Fatalf("SpawnFormattedIn: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d, want > 0", pid)
	}
	body := waitForLogContainsAndExit(t, logPath, "HELLO", "WORLD")
	helloAt := strings.Index(body, "HELLO")
	worldAt := strings.Index(body, "WORLD")
	exitAt := strings.Index(body, "child exit")
	if helloAt < 0 || worldAt < 0 || exitAt < 0 {
		t.Fatalf("missing markers: %q", body)
	}
	if helloAt >= worldAt || worldAt >= exitAt {
		t.Fatalf("unexpected order helloAt=%d worldAt=%d exitAt=%d: %q",
			helloAt, worldAt, exitAt, body)
	}
}

// TestSpawnFormattedIn_NilFormatterIsIdentity pins the nil-formatter
// fall-through: bytes pass through unchanged.
func TestSpawnFormattedIn_NilFormatterIsIdentity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := SpawnFormattedIn(
		t.Context(), "", logPath, nil, "sh", "-c", "echo verbatim")
	if err != nil {
		t.Fatalf("SpawnFormattedIn: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	waitForLogContains(t, logPath, "verbatim")
}

// TestSpawnFormattedIn_MultiLineFormatter pins the case where one
// source line maps to multiple output lines (e.g. claude's
// multi-content-block assistant events). Each rendered line lands
// in agent.log in the order the formatter emitted them.
func TestSpawnFormattedIn_MultiLineFormatter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	expand := func(line []byte) []byte {
		s := strings.TrimRight(string(line), "\r\n")
		return fmt.Appendf(nil, "a:%s\nb:%s\n", s, s)
	}
	if _, err := SpawnFormattedIn(
		t.Context(), "", logPath, expand,
		"sh", "-c", "echo X; echo Y",
	); err != nil {
		t.Fatalf("SpawnFormattedIn: %v", err)
	}
	body := waitForLogContainsAndExit(t, logPath,
		"a:X", "b:X", "a:Y", "b:Y")
	for _, want := range []string{"a:X", "b:X", "a:Y", "b:Y"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q: %q", want, body)
		}
	}
	if strings.Index(body, "a:X") > strings.Index(body, "b:X") ||
		strings.Index(body, "b:X") > strings.Index(body, "a:Y") ||
		strings.Index(body, "a:Y") > strings.Index(body, "b:Y") {
		t.Fatalf("out of order: %q", body)
	}
}

// TestSpawnFormattedIn_ChildExitAfterDrain pins the synchronisation
// contract: the `child exit` marker lands strictly after the last
// formatted line. A slow formatter that sleeps per call would
// otherwise let the reaper race ahead.
func TestSpawnFormattedIn_ChildExitAfterDrain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	slow := func(line []byte) []byte {
		time.Sleep(40 * time.Millisecond)
		return append([]byte("fmt:"), line...)
	}
	if _, err := SpawnFormattedIn(
		t.Context(), "", logPath, slow,
		"sh", "-c", "echo last-line",
	); err != nil {
		t.Fatalf("SpawnFormattedIn: %v", err)
	}
	body := waitForLogContainsAndExit(t, logPath,
		"fmt:last-line", "child exit")
	if strings.Index(body, "fmt:last-line") >
		strings.Index(body, "child exit") {
		t.Fatalf("child exit landed before drain flush: %q", body)
	}
}

// TestSpawnFormattedIn_EmptyLogPath pins the empty-path guard.
func TestSpawnFormattedIn_EmptyLogPath(t *testing.T) {
	t.Parallel()
	_, err := SpawnFormattedIn(
		t.Context(), "", "", nil, "true")
	if err == nil ||
		!strings.Contains(err.Error(), "empty log path") {
		t.Fatalf("err = %v", err)
	}
}

// TestSpawnFormattedIn_LogOpenFails pins the OpenFile error path:
// passing a directory as logPath fails before the child is started.
func TestSpawnFormattedIn_LogOpenFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := SpawnFormattedIn(
		t.Context(), "", dir, nil, "true",
	); err == nil {
		t.Fatal("expected open error when logPath is a directory")
	}
}

// TestSpawnFormattedIn_MissingBinary pins the cmd.Start error path.
func TestSpawnFormattedIn_MissingBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	_, err := SpawnFormattedIn(
		t.Context(), "", logPath, nil,
		"/no/such/binary-spawnformatted-xyz")
	if err == nil {
		t.Fatal("expected error from missing binary")
	}
	if !strings.Contains(err.Error(),
		"binary-spawnformatted-xyz") {
		t.Fatalf("err = %v", err)
	}
}

// TestSpawnFormattedIn_PartialLineAtEOF pins the no-trailing-newline
// branch: a child that crashes mid-line still has its last partial
// chunk delivered to the formatter so panic / runtime error text
// survives in agent.log.
func TestSpawnFormattedIn_PartialLineAtEOF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	if _, err := SpawnFormattedIn(
		t.Context(), "", logPath, nil,
		"sh", "-c", "printf 'no-newline'",
	); err != nil {
		t.Fatalf("SpawnFormattedIn: %v", err)
	}
	waitForLogContains(t, logPath, "no-newline")
}

func TestDrainFormatted_AllowsSuppressedLines(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	logFile, err := os.OpenFile(
		logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go drainFormatted(&formattedRun{
		LogFile:   logFile,
		DrainDone: done,
	}, pr, func([]byte) []byte { return nil })
	if _, err := pw.WriteString("hidden\n"); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	<-done
	if err := logFile.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("log = %q, want empty", data)
	}
}

// waitForLogContainsAndExit polls logPath for every needle plus the
// `child exit` marker and returns the file body.
func waitForLogContainsAndExit(
	t *testing.T, logPath string, needles ...string,
) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(logPath)
		body := string(data)
		hit := strings.Contains(body, "child exit")
		for _, n := range needles {
			if !strings.Contains(body, n) {
				hit = false
				break
			}
		}
		if hit {
			return body
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %v + child_exit: %q",
				needles, body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
