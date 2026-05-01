//go:build !windows

package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutput_Success(t *testing.T) {
	out, err := Output(context.Background(), "echo", "hi")
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("output = %q", out)
	}
}

func TestOutput_Failure_WithStderr(t *testing.T) {
	// `ls /no/such/path` exits non-zero and writes a clear error to
	// stderr across BSD and GNU coreutils, so the wrapped message is
	// exercised.
	_, err := Output(context.Background(), "ls", "/no/such/path/should/not/exist")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ls") {
		t.Fatalf("err = %v", err)
	}
}

func TestOutput_Failure_StdoutOnly(t *testing.T) {
	// A shell snippet that fails non-zero and writes to stdout but not
	// stderr exercises the stderr-empty-but-stdout-nonempty fallback.
	_, err := Output(context.Background(), "sh", "-c", "echo stdoutmsg; exit 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stdoutmsg") {
		t.Fatalf("err = %v", err)
	}
}

func TestOutput_Failure_NoStderr(t *testing.T) {
	// `false` exits non-zero with no stdout/stderr, exercising the
	// both-empty fallback path.
	_, err := Output(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_Success(t *testing.T) {
	// `true` inherits stdin/stdout/stderr (so nothing is written) and
	// exits zero; exercises the success path of Run.
	if err := Run(context.Background(), "true"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRun_Failure(t *testing.T) {
	// `false` exits non-zero with no output; exercises Run's error wrap.
	err := Run(context.Background(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Fatalf("err = %v", err)
	}
}

// TestSpawn_Success exercises the happy path: Spawn returns a positive
// PID immediately, then the briefly-running child writes to the log
// and exits. Polling the log is the only way to observe the child
// because Spawn deliberately calls Process.Release so the parent
// cannot Wait.
func TestSpawn_Success(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := Spawn(context.Background(), logPath, "sh", "-c", "echo hello-spawn; echo world >&2")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d, want > 0", pid)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, readErr := os.ReadFile(logPath)
		if readErr == nil && strings.Contains(string(data), "hello-spawn") && strings.Contains(string(data), "world") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: log = %q (err=%v)", data, readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSpawn_EmptyLogPath pins the empty-path guard.
func TestSpawn_EmptyLogPath(t *testing.T) {
	_, err := Spawn(context.Background(), "", "true")
	if err == nil || !strings.Contains(err.Error(), "empty log path") {
		t.Fatalf("err = %v", err)
	}
}

// TestSpawn_LogOpenFails covers the "log path is a directory" error
// branch: OpenFile rejects the path, so Spawn returns before fork
// and the wrapped error mentions the path.
func TestSpawn_LogOpenFails(t *testing.T) {
	dir := t.TempDir()
	if _, err := Spawn(context.Background(), dir, "true"); err == nil {
		t.Fatal("expected open error when logPath is a directory")
	}
}

// TestSpawn_MissingBinary covers the cmd.Start error path: a
// non-existent binary on PATH fails fork/exec and the wrapped error
// mentions the requested name.
func TestSpawn_MissingBinary(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	_, err := Spawn(context.Background(), logPath, "/no/such/cursor-agent-binary-xyzzy")
	if err == nil {
		t.Fatal("expected error from missing binary")
	}
	if !strings.Contains(err.Error(), "cursor-agent-binary-xyzzy") {
		t.Fatalf("err = %v", err)
	}
}

// TestIsAlive_LiveAndDead drives both branches with a real short-lived
// `sleep` child started via os/exec: the helper reports true while
// the child is asleep and false after Wait has reaped it. Going
// through os/exec (rather than Spawn) lets the test Wait on the
// child and avoid the zombie state that Spawn's Process.Release
// leaves behind.
func TestIsAlive_LiveAndDead(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	if !IsAlive(pid) {
		t.Fatalf("expected pid %d to be alive after Start", pid)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatalf("Wait: expected non-nil error after Kill")
	}
	if IsAlive(pid) {
		t.Fatalf("expected pid %d to be dead after Wait", pid)
	}
}

// TestRunIn_Success_PlumbsCwd asserts RunIn sets cmd.Dir on the
// child: pwd inside a temp dir prints that dir on stdout, which the
// child writes to a file (Run inherits the parent's stdout but the
// test runs in `go test` so stdout is captured; instead the child
// records cwd to a file via shell redirection).
func TestRunIn_Success_PlumbsCwd(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "cwd.txt")
	if err := RunIn(context.Background(), dir, "sh", "-c", "pwd > "+out); err != nil {
		t.Fatalf("RunIn: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read cwd file: %v", err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		want = dir
	}
	gotResolved, err := filepath.EvalSymlinks(strings.TrimSpace(string(got)))
	if err != nil {
		gotResolved = strings.TrimSpace(string(got))
	}
	if gotResolved != want {
		t.Fatalf("cwd = %q, want %q", gotResolved, want)
	}
}

// TestRunIn_EmptyDir_InheritsParentCwd pins the empty-dir branch:
// passing "" leaves cmd.Dir empty so the child inherits the parent's
// CWD (the same behaviour as Run).
func TestRunIn_EmptyDir_InheritsParentCwd(t *testing.T) {
	if err := RunIn(context.Background(), "", "true"); err != nil {
		t.Fatalf("RunIn: %v", err)
	}
}

// TestSpawnIn_PlumbsCwd asserts SpawnIn sets cmd.Dir on the spawned
// child. The child writes pwd to the log path, which the parent
// polls for the expected directory.
func TestSpawnIn_PlumbsCwd(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := SpawnIn(context.Background(), dir, logPath, "sh", "-c", "pwd")
	if err != nil {
		t.Fatalf("SpawnIn: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d, want > 0", pid)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		want = dir
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, readErr := os.ReadFile(logPath)
		if readErr == nil && len(strings.TrimSpace(string(data))) > 0 {
			got, evalErr := filepath.EvalSymlinks(strings.TrimSpace(string(data)))
			if evalErr != nil {
				got = strings.TrimSpace(string(data))
			}
			if got != want {
				t.Fatalf("cwd = %q, want %q", got, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: log = %q (err=%v)", data, readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSpawnIn_EmptyLogPath pins the empty-log-path guard, mirroring
// TestSpawn_EmptyLogPath.
func TestSpawnIn_EmptyLogPath(t *testing.T) {
	_, err := SpawnIn(context.Background(), "", "", "true")
	if err == nil || !strings.Contains(err.Error(), "empty log path") {
		t.Fatalf("err = %v", err)
	}
}

// TestIsAlive_NonPositive covers the pid <= 0 short-circuit.
func TestIsAlive_NonPositive(t *testing.T) {
	if IsAlive(0) {
		t.Fatal("IsAlive(0) should be false")
	}
	if IsAlive(-1) {
		t.Fatal("IsAlive(-1) should be false")
	}
}

// TestIsAlive_KnownDead picks an unlikely PID and asserts IsAlive
// reports false. PID 0x7fffffff is reserved (well above any plausible
// kernel max-pid), so signal(0) returns ESRCH and the helper reports
// dead. If a system somehow has that PID assigned the test skips.
func TestIsAlive_KnownDead(t *testing.T) {
	const unlikely = 0x7fffffff
	if IsAlive(unlikely) {
		t.Skip("PID 0x7fffffff is unexpectedly alive on this system")
	}
}
