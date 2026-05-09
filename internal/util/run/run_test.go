package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestOutput_Success(t *testing.T) {
	t.Parallel()
	out, err := Output(t.Context(), "echo", "hi")
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("output = %q", out)
	}
}

func TestOutput_Failure_WithStderr(t *testing.T) {
	t.Parallel()
	// `ls /no/such/path` exits non-zero and writes a clear error to
	// stderr across BSD and GNU coreutils, so the wrapped message is
	// exercised.
	_, err := Output(t.Context(), "ls", "/no/such/path/should/not/exist")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ls") {
		t.Fatalf("err = %v", err)
	}
}

func TestOutput_Failure_StdoutOnly(t *testing.T) {
	t.Parallel()
	// A shell snippet that fails non-zero and writes to stdout but not
	// stderr exercises the stderr-empty-but-stdout-nonempty fallback.
	_, err := Output(t.Context(), "sh", "-c", "echo stdoutmsg; exit 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "stdoutmsg") {
		t.Fatalf("err = %v", err)
	}
}

func TestOutput_Failure_NoStderr(t *testing.T) {
	t.Parallel()
	// `false` exits non-zero with no stdout/stderr, exercising the
	// both-empty fallback path.
	_, err := Output(t.Context(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Fatalf("err = %v", err)
	}
}

func TestRun_Success(t *testing.T) {
	t.Parallel()
	// `true` inherits stdin/stdout/stderr (so nothing is written) and
	// exits zero; exercises the success path of Run.
	if err := Run(t.Context(), "true"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRun_Failure(t *testing.T) {
	t.Parallel()
	// `false` exits non-zero with no output; exercises Run's error wrap.
	err := Run(t.Context(), "false")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "false") {
		t.Fatalf("err = %v", err)
	}
}

// TestSpawn_Success exercises the happy path: Spawn returns a positive
// PID immediately, then the briefly-running child writes to the log
// and exits. Polling the log is the only way for the parent to
// observe the child — Spawn does not expose a Wait handle (a
// background goroutine inside Spawn calls cmd.Wait so the kernel
// reaps the child instead of leaving a zombie behind).
func TestSpawn_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := Spawn(t.Context(), logPath, "sh", "-c", "echo hello-spawn; echo world >&2")
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

// TestSpawn_AppendsAcrossInvocations pins the append-only invariant on
// the shared per-task agent.log: a second SpawnIn against the same
// path must preserve every byte written by the first child.
// Orchestrator + planner + worker + verifier all share one log file
// across phase boundaries (and every retry iteration of the
// worker→verifier loop), and tests downstream rely on the earlier
// phase's bytes surviving.
func TestSpawn_AppendsAcrossInvocations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	if _, err := Spawn(t.Context(), logPath, "sh", "-c", "echo first-line"); err != nil {
		t.Fatalf("first Spawn: %v", err)
	}
	waitForLogContains(t, logPath, "first-line")
	if _, err := Spawn(t.Context(), logPath, "sh", "-c", "echo second-line"); err != nil {
		t.Fatalf("second Spawn: %v", err)
	}
	waitForLogContains(t, logPath, "second-line")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "first-line") {
		t.Fatalf("first child's bytes were truncated: %q", got)
	}
	if !strings.Contains(got, "second-line") {
		t.Fatalf("second child's bytes are missing: %q", got)
	}
	if strings.Index(got, "first-line") > strings.Index(got, "second-line") {
		t.Fatalf("expected chronological order, got %q", got)
	}
}

// waitForLogContains polls logPath until it contains needle or the
// deadline elapses.
func waitForLogContains(t *testing.T, logPath, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), needle) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %q in %s: log=%q err=%v", needle, logPath, string(data), err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSpawn_EmptyLogPath pins the empty-path guard.
func TestSpawn_EmptyLogPath(t *testing.T) {
	t.Parallel()
	_, err := Spawn(t.Context(), "", "true")
	if err == nil || !strings.Contains(err.Error(), "empty log path") {
		t.Fatalf("err = %v", err)
	}
}

// TestSpawn_LogOpenFails covers the "log path is a directory" error
// branch: OpenFile rejects the path, so Spawn returns before fork
// and the wrapped error mentions the path.
func TestSpawn_LogOpenFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Spawn(t.Context(), dir, "true"); err == nil {
		t.Fatal("expected open error when logPath is a directory")
	}
}

// TestSpawn_MissingBinary covers the cmd.Start error path: a
// non-existent binary on PATH fails fork/exec and the wrapped error
// mentions the requested name.
func TestSpawn_MissingBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	_, err := Spawn(t.Context(), logPath, "/no/such/cursor-agent-binary-xyzzy")
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
// through os/exec (rather than Spawn) keeps the test in full control
// of when the child is reaped, so the alive→dead transition observed
// by IsAlive is deterministic.
func TestIsAlive_LiveAndDead(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "cwd.txt")
	if err := RunIn(t.Context(), dir, "sh", "-c", "pwd > "+out); err != nil {
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
	t.Parallel()
	if err := RunIn(t.Context(), "", "true"); err != nil {
		t.Fatalf("RunIn: %v", err)
	}
}

// TestSpawnIn_PlumbsCwd asserts SpawnIn sets cmd.Dir on the spawned
// child. The child writes pwd to the log path, which the parent
// polls for the expected directory.
func TestSpawnIn_PlumbsCwd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := SpawnIn(t.Context(), dir, logPath, "sh", "-c", "pwd")
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
		if readErr == nil {
			if firstLine := firstNonMarkerLine(string(data)); firstLine != "" {
				got, evalErr := filepath.EvalSymlinks(firstLine)
				if evalErr != nil {
					got = firstLine
				}
				if got != want {
					t.Fatalf("cwd = %q, want %q", got, want)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout: log = %q (err=%v)", data, readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// markerLine matches an agentlog marker header at the start of a line:
// RFC3339Z timestamp followed by two spaces. SpawnIn appends a
// `child exit` marker after the child reaps, so tests that consume
// the child's own stdout must skip marker lines to avoid mixing the
// two streams.
var markerLine = regexp.MustCompile(
	`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z  `,
)

// firstNonMarkerLine returns the first non-empty line of data that is
// not an agentlog marker (RFC3339Z + two spaces).
func firstNonMarkerLine(data string) string {
	for line := range strings.SplitSeq(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || markerLine.MatchString(trimmed) {
			continue
		}
		return trimmed
	}
	return ""
}

// TestSpawn_AppendsChildExitMarker pins the child_exit marker
// invariant: after a Spawn-ed child exits, the reap goroutine must
// append one human-readable `child exit` marker line to the same log
// so a tailer can see the child's exit code without opening bbolt.
func TestSpawn_AppendsChildExitMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := Spawn(t.Context(), logPath, "sh", "-c", "exit 0")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(logPath)
		body := string(data)
		if strings.Contains(body, "child exit") {
			if !strings.Contains(body, "exit_code=0") {
				t.Fatalf("missing exit_code in marker: %q", body)
			}
			pidNeedle := fmt.Sprintf("pid=%d", pid)
			if !strings.Contains(body, pidNeedle) {
				t.Fatalf("missing pid %d in marker: %q", pid, body)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"timeout waiting for child_exit marker: log=%q", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSpawnIn_EmptyLogPath pins the empty-log-path guard, mirroring
// TestSpawn_EmptyLogPath.
func TestSpawnIn_EmptyLogPath(t *testing.T) {
	t.Parallel()
	_, err := SpawnIn(t.Context(), "", "", "true")
	if err == nil || !strings.Contains(err.Error(), "empty log path") {
		t.Fatalf("err = %v", err)
	}
}

// TestWaitForExit_ZeroPid pins the immediate-nil branch for the
// "synchronous / nothing to wait on" sentinel.
func TestWaitForExit_ZeroPid(t *testing.T) {
	t.Parallel()
	if err := WaitForExit(t.Context(), 0); err != nil {
		t.Fatalf("WaitForExit(0) = %v, want nil", err)
	}
	if err := WaitForExit(t.Context(), -1); err != nil {
		t.Fatalf("WaitForExit(-1) = %v, want nil", err)
	}
}

// TestWaitForExit_AlreadyDead drives the immediate-nil branch when
// IsAlive reports false on the very first check (no ticker spin).
// Pid 0x7fffffff is reserved well above any plausible kernel
// max-pid, mirroring TestIsAlive_KnownDead.
func TestWaitForExit_AlreadyDead(t *testing.T) {
	t.Parallel()
	const unlikely = 0x7fffffff
	if IsAlive(unlikely) {
		t.Skip("PID 0x7fffffff is unexpectedly alive on this system")
	}
	if err := WaitForExit(t.Context(), unlikely); err != nil {
		t.Fatalf("WaitForExit(dead) = %v, want nil", err)
	}
}

// TestWaitForExit_LiveExitsDuringPoll starts a real short-lived
// child via os/exec, reaps it via cmd.Wait in a goroutine to match
// the Spawn contract (no zombie), and asserts WaitForExit blocks
// until the child exits.
func TestWaitForExit_LiveExitsDuringPoll(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("sleep", "0.3")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	// Drive the post-Start branch: child is still alive on entry,
	// so WaitForExit must spin the ticker. Wait in a goroutine so
	// the kernel reaps the child and IsAlive flips to false
	// deterministically — this is the same reap pattern Spawn now
	// applies internally.
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	start := time.Now()
	if err := WaitForExit(t.Context(), pid); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatalf("WaitForExit returned too fast (%v); should have polled the ticker", time.Since(start))
	}
	<-waitDone
	if IsAlive(pid) {
		t.Fatalf("pid %d should be dead after WaitForExit returned", pid)
	}
}

// TestSpawn_AndWaitForExit_ReapsZombie pins the regression behind the
// "j tasks start chains plan→work→verify but stalls at planning" bug:
// before the fix, Spawn called Process.Release after Start, so the
// kernel never reaped the exited child. signal(0) on a zombie returns
// success on Linux/macOS, so IsAlive reported the dead child alive
// and WaitForExit polled forever. The fix has Spawn reap via cmd.Wait
// in a goroutine; this test asserts WaitForExit returns promptly
// (well under the ctx deadline) on a real Spawned child that exits
// quickly. Without the fix this would hang until the deadline.
func TestSpawn_AndWaitForExit_ReapsZombie(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "out.log")
	pid, err := Spawn(t.Context(), logPath, "sh", "-c", "sleep 0.1")
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d, want > 0", pid)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := WaitForExit(ctx, pid); err != nil {
		t.Fatalf("WaitForExit: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("WaitForExit took %v; expected to return well under the 2s deadline (zombie reap regression)", elapsed)
	}
	if IsAlive(pid) {
		t.Fatalf("pid %d still reported alive after WaitForExit returned", pid)
	}
}

// TestWaitForExit_CtxCancelled drives the ctx.Done branch: a long-
// running sleep is started, then the parent context is cancelled
// before the child exits. WaitForExit must return ctx.Err().
func TestWaitForExit_CtxCancelled(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	err := WaitForExit(ctx, pid)
	if err == nil {
		t.Fatal("WaitForExit must surface ctx.Err() when the parent cancels")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestIsAlive_NonPositive covers the pid <= 0 short-circuit.
func TestIsAlive_NonPositive(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	const unlikely = 0x7fffffff
	if IsAlive(unlikely) {
		t.Skip("PID 0x7fffffff is unexpectedly alive on this system")
	}
}
