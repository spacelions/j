package testcases_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	clitasks "github.com/spacelions/j/internal/cli/tasks"
	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
	"github.com/spacelions/j/internal/testutil"
)

const (
	lockTakeoverHolderKey = "J_LOCK_TAKEOVER_HOLDER"
	lockTakeoverPathKey   = "J_LOCK_TAKEOVER_PATH"
)

// TestResumeTakeoverLockedTask verifies AC6 and AC7: RunResumeWork
// finds the per-task flock held by a real child process, terminates
// it via the production run.Terminate path (SIGTERM then SIGKILL),
// waits for the lock to drain, and invokes the stub orchestrator.
// Stderr must contain the takeover note naming the holder's pid.
// The child process is gone within 5 s of RunResumeWork returning.
//
// When spawned as the lock-holding child (lockTakeoverHolderKey=1)
// this function acquires the lock at lockTakeoverPathKey and sleeps
// until SIGTERM is received.
func TestResumeTakeoverLockedTask(t *testing.T) {
	if os.Getenv(lockTakeoverHolderKey) == "1" {
		lockHolderRunAt(os.Getenv(lockTakeoverPathKey))
		return
	}
	recoverySetupEnv(t)
	id := recoverySeedTask(t, func(task *tasks.Task) {
		task.Status = tasks.StatusCompleted
		task.WorkResumeSession = "active-session"
	})
	taskDir, err := tasks.EnsureDir(id)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	lockPath := filepath.Join(taskDir, tasks.LockFileName)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe,
		"-test.run", "^TestResumeTakeoverLockedTask$",
	)
	cmd.Env = append(os.Environ(),
		lockTakeoverHolderKey+"=1",
		lockTakeoverPathKey+"="+lockPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	holderPID := cmd.Process.Pid
	// Reap goroutine removes the zombie so run.IsAlive sees the child
	// as dead once the kernel releases the flock on process exit.
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	lockWaitForHolder(t, lockPath)

	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	var stderr bytes.Buffer
	err = clitasks.RunResumeWork(
		t.Context(), clitasks.ResumeWorkOptions{
			Stdin:  strings.NewReader(""),
			Stdout: io.Discard,
			Stderr: &stderr,
			Agents: []codingagents.Agent{
				testutil.NewScriptedAgent(),
			},
			UI:      &recoveryFakeUI{pickReturn: id},
			JBinary: recoveryArgvJStub(t, argvPath),
		},
	)
	if err != nil {
		t.Fatalf("RunResumeWork: %v", err)
	}

	// AC6: stderr includes "taking over task <id> from pid <n>".
	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "taking over task "+id) {
		t.Errorf(
			"AC6: stderr missing takeover note for task; got %q",
			stderrStr,
		)
	}
	wantPID := fmt.Sprintf("pid %d", holderPID)
	if !strings.Contains(stderrStr, wantPID) {
		t.Errorf(
			"AC6: stderr missing holder pid %d; got %q",
			holderPID, stderrStr,
		)
	}

	// AC7: in-flight phase is fully gone — child process is dead.
	lockAssertChildGone(t, holderPID, 5*time.Second)

	_ = recoveryReadStubArgv(t, argvPath)
}

// lockHolderRunAt opens the file at path, takes an exclusive
// non-blocking flock, writes TOML holder metadata so the contention
// message can name the pid and phase, then sleeps until SIGTERM.
// Called from child processes only; exits on any failure.
func lockHolderRunAt(path string) {
	if path == "" {
		fmt.Fprintln(os.Stderr, "lock holder: path env not set")
		os.Exit(1)
	}
	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lock holder open: %v\n", err)
		os.Exit(1)
	}
	if err := syscall.Flock(
		int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB,
	); err != nil {
		fmt.Fprintf(os.Stderr, "lock holder flock: %v\n", err)
		os.Exit(1)
	}
	host, _ := os.Hostname()
	meta := fmt.Sprintf(
		"pid = %d\nhost = %q\nphase = \"working\"\n"+
			"started_at = %s\n",
		os.Getpid(), host,
		time.Now().UTC().Format(time.RFC3339),
	)
	_ = os.WriteFile(path, []byte(meta), 0o644)
	fmt.Printf("HELD pid=%d\n", os.Getpid())
	time.Sleep(60 * time.Second)
}

// lockWaitForHolder polls lockPath until the TOML metadata written by
// the child holder appears with a non-zero pid, or the 5-second
// deadline expires and the test fails.
func lockWaitForHolder(t *testing.T, lockPath string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(lockPath)
		for line := range strings.SplitSeq(string(data), "\n") {
			var pid int
			_, e := fmt.Sscanf(line, "pid = %d", &pid)
			if e == nil && pid > 0 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("child never acquired the per-task lock")
}

// lockAssertChildGone waits up to timeout for pid to exit, then
// reports an error if it is still alive.
func lockAssertChildGone(
	t *testing.T, pid int, timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		proc, _ := os.FindProcess(pid)
		if proc == nil {
			return
		}
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf(
		"AC7: holder pid %d still alive %s after takeover",
		pid, timeout,
	)
}
