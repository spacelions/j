package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func setupLockProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	jDir := filepath.Join(root, ".j")
	if err := os.MkdirAll(filepath.Join(jDir, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return jDir
}

func TestAcquireLock_SucceedsOnFreshDir(t *testing.T) {
	setupLockProject(t)
	ctx := WithPhase(context.Background(), "planning")
	l, err := AcquireLock(ctx, "T1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer func() { _ = l.Release() }()
	h := readHolder(l.metaPath)
	if h.PID != os.Getpid() {
		t.Fatalf("pid: got %d want %d", h.PID, os.Getpid())
	}
	if h.Phase != "planning" {
		t.Fatalf("phase: got %q", h.Phase)
	}
	if h.StartedAt.IsZero() {
		t.Fatalf("started_at zero")
	}
}

func TestAcquireLock_ContentionReturnsLockedError(t *testing.T) {
	setupLockProject(t)
	ctx := WithPhase(context.Background(), "planning")
	first, err := AcquireLock(ctx, "T1")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer func() { _ = first.Release() }()
	second, err := AcquireLock(ctx, "T1")
	if err == nil {
		_ = second.Release()
		t.Fatalf("second acquire: want error, got nil")
	}
	var locked *LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("err type: got %T want *LockedError", err)
	}
	if locked.Holder.PID != os.Getpid() {
		t.Fatalf("holder pid: %d", locked.Holder.PID)
	}
	if locked.Holder.Phase != "planning" {
		t.Fatalf("holder phase: %q", locked.Holder.Phase)
	}
	if !errors.Is(locked, syscall.EWOULDBLOCK) {
		t.Fatalf("unwrap: want EWOULDBLOCK, got %v", locked.Unwrap())
	}
}

func TestAcquireLock_ReleaseAllowsReacquire(t *testing.T) {
	setupLockProject(t)
	ctx := WithPhase(context.Background(), "planning")
	l1, err := AcquireLock(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	if err := l1.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := l1.Release(); err != nil {
		t.Fatalf("idempotent release: %v", err)
	}
	ctx2 := WithPhase(context.Background(), "working")
	l2, err := AcquireLock(ctx2, "T1")
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	defer func() { _ = l2.Release() }()
	h := readHolder(l2.metaPath)
	if h.Phase != "working" {
		t.Fatalf("phase rewritten: got %q", h.Phase)
	}
}

func TestLockUpdatePhase(t *testing.T) {
	setupLockProject(t)
	ctx := WithPhase(context.Background(), "planning")
	l, err := AcquireLock(ctx, "T1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Release() }()
	if err := l.UpdatePhase("verifying"); err != nil {
		t.Fatalf("UpdatePhase: %v", err)
	}
	if h := readHolder(l.metaPath); h.Phase != "verifying" {
		t.Fatalf("phase = %q, want verifying", h.Phase)
	}
	var nilLock *Lock
	if err := nilLock.UpdatePhase("working"); err != nil {
		t.Fatalf("nil UpdatePhase: %v", err)
	}
}

// TestAcquireLock_AfterHolderKilled exercises the crash-recovery path.
// It spawns a child process via go-test's own binary (with a sentinel
// env var) that acquires the lock then sleeps; the parent SIGKILLs it
// and asserts the next AcquireLock succeeds without --force.
func TestAcquireLock_AfterHolderKilled(t *testing.T) {
	if os.Getenv("J_LOCK_TEST_HOLDER") == "1" {
		runLockHolderChild()
		return
	}
	jDir := setupLockProject(t)
	taskDir := filepath.Join(jDir, DirName, "T1")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(taskDir, LockFileName)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run", t.Name())
	cmd.Env = append(os.Environ(),
		"J_LOCK_TEST_HOLDER=1",
		"J_LOCK_TEST_PATH="+lockPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	waitForHolder(t, lockPath)
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.Process.Wait(); err != nil {
		t.Logf("wait: %v", err)
	}
	l, err := AcquireLock(context.Background(), "T1")
	if err != nil {
		t.Fatalf("post-kill acquire: %v", err)
	}
	_ = l.Release()
}

func runLockHolderChild() {
	path := os.Getenv("J_LOCK_TEST_PATH")
	l, err := acquireLockAt(
		WithPhase(context.Background(), "planning"), path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "child acquire: %v\n", err)
		os.Exit(1)
	}
	_ = l
	fmt.Fprintf(os.Stdout, "HELD pid=%d\n", os.Getpid())
	time.Sleep(60 * time.Second)
}

func waitForHolder(t *testing.T, lockPath string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if h := readHolder(lockPath); h.PID > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("holder never appeared at %s", lockPath)
}

func TestAcquireLock_ConcurrentExactlyOneWins(t *testing.T) {
	setupLockProject(t)
	const N = 8
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	locks := make(chan *Lock, N)
	for range N {
		wg.Go(func() {
			<-start
			l, err := AcquireLock(context.Background(), "T1")
			if err == nil {
				wins.Add(1)
				locks <- l
			}
		})
	}
	close(start)
	wg.Wait()
	close(locks)
	for l := range locks {
		_ = l.Release()
	}
	if wins.Load() != 1 {
		t.Fatalf("wins=%d want 1", wins.Load())
	}
}

func TestTryAcquireForReap_NoFileReturnsNil(t *testing.T) {
	setupLockProject(t)
	l, err := TryAcquireForReap("T1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if l != nil {
		_ = l.Release()
		t.Fatalf("expected nil lock when no .lock file")
	}
}

func TestTryAcquireForReap_HeldReturnsNil(t *testing.T) {
	setupLockProject(t)
	held, err := AcquireLock(context.Background(), "T1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Release() }()
	got, err := TryAcquireForReap("T1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		_ = got.Release()
		t.Fatalf("expected nil when held")
	}
}

func TestTryAcquireForReap_StaleFileSucceeds(t *testing.T) {
	jDir := setupLockProject(t)
	taskDir := filepath.Join(jDir, DirName, "T1")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := Holder{
		PID: 999999, Host: "h", Phase: "planning",
		StartedAt: time.Now().UTC(),
	}
	if err := writeHolder(
		filepath.Join(taskDir, LockFileName), stale); err != nil {
		t.Fatal(err)
	}
	l, err := TryAcquireForReap("T1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if l == nil {
		t.Fatalf("expected acquire on stale file")
	}
	_ = l.Release()
	h := readHolder(filepath.Join(taskDir, LockFileName))
	if h.PID != 999999 {
		t.Fatalf("metadata rewritten: pid=%d", h.PID)
	}
}

// TestReadHolder_InvalidTOMLReturnsZero drives the Unmarshal error
// branch by writing junk that decodes neither as a Holder nor as a
// valid TOML document.
func TestReadHolder_InvalidTOMLReturnsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LockFileName)
	if err := os.WriteFile(path, []byte("this is not toml\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readHolder(path); got.PID != 0 || got.Host != "" {
		t.Fatalf("readHolder = %+v, want zero Holder on invalid TOML", got)
	}
}

func TestPhaseFromContext_NilSafe(t *testing.T) {
	//nolint:staticcheck // testing the nil-safety contract
	if PhaseFromContext(nil) != "" {
		t.Fatalf("nil ctx: want empty")
	}
	if PhaseFromContext(context.Background()) != "" {
		t.Fatalf("no value: want empty")
	}
}

func TestLockedError_Error(t *testing.T) {
	e := &LockedError{Holder: Holder{
		PID: 42, Host: "x", Phase: "p",
		StartedAt: time.Unix(0, 0).UTC(),
	}, cause: syscall.EWOULDBLOCK}
	if e.Error() == "" {
		t.Fatal("empty")
	}
}

// TestWriteHolder_WriteError covers the os.WriteFile error branch by
// making the target directory read-only before the write.
func TestWriteHolder_WriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := writeHolder(filepath.Join(dir, "lock"), Holder{PID: 1}); err == nil {
		t.Fatal("expected writeHolder to fail when dir is not writable")
	}
}
