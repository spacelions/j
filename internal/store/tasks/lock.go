package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// LockFileName is the per-task advisory `flock` file. The kernel lock
// on this file (LOCK_EX) coordinates concurrent `j` orchestrators
// against the same task id: the holder owns the right to mutate
// requirements.md / plan.md / agent.log / task.toml for the task.
// The file's contents are an informational TOML snapshot of the
// current holder (pid, host, phase, started_at). Crash safety is free
// because the kernel releases the lock on process death; callers do
// not need a `--force` unlock flag.
const LockFileName = ".lock"

// Holder is the informational metadata written into the per-task lock
// file when AcquireLock succeeds. It is read back on contention so
// the friendly "task already in use by ..." message can name who is
// holding it. Crash-safe: the kernel-level flock is the source of
// truth; this metadata is observability only.
type Holder struct {
	PID       int       `toml:"pid"`
	Host      string    `toml:"host"`
	Phase     string    `toml:"phase,omitempty"`
	StartedAt time.Time `toml:"started_at"`
}

// LockedError is returned by AcquireLock when another process already
// holds the per-task lock. Holder carries the on-disk metadata of the
// existing owner so callers can print a useful contention message and
// `errors.As` lets resume / re-* commands branch on the takeover path.
type LockedError struct {
	Holder Holder
	cause  error
}

// Error implements the error interface.
func (e *LockedError) Error() string {
	return fmt.Sprintf(
		"tasks: locked by pid %d on %s (phase %q, since %s)",
		e.Holder.PID, e.Holder.Host, e.Holder.Phase,
		e.Holder.StartedAt.Format(time.RFC3339),
	)
}

// Unwrap exposes the underlying syscall error so errors.Is /
// errors.As can match against syscall.EWOULDBLOCK when callers want
// the raw kernel reason rather than the friendly Holder snapshot.
func (e *LockedError) Unwrap() error { return e.cause }

// Lock is a per-task advisory flock holder. Construct one with
// AcquireLock; release it with Release (idempotent, defer-safe).
type Lock struct {
	fd       *os.File
	metaPath string
	holder   Holder
}

type phaseCtxKey struct{}

// WithPhase derives a context tagged with the orchestration phase
// (planning / working / verifying / resuming-plan / ...). AcquireLock
// reads the value off the context and writes it into the lock file's
// holder metadata so contention messages can name the active phase.
func WithPhase(ctx context.Context, phase string) context.Context {
	return context.WithValue(ctx, phaseCtxKey{}, phase)
}

// PhaseFromContext returns the phase tag attached via WithPhase, or
// the empty string when no phase has been recorded.
func PhaseFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(phaseCtxKey{}).(string)
	return v
}

// AcquireLock takes the per-task `flock` and writes the holder
// metadata. On contention it returns *LockedError carrying the existing
// holder snapshot. Other syscall failures bubble up wrapped.
//
// EnsureDir is invoked first so callers do not have to mkdir the
// per-task directory before locking — `j tasks resume-*` and the
// reaper both want this idempotent behaviour. The lock file is
// O_CREATE|O_RDWR; the kernel-level lock is the source of truth, so
// the caller must Release in defer to release the fd.
func AcquireLock(ctx context.Context, id string) (*Lock, error) {
	taskDir, err := EnsureDir(id)
	if err != nil {
		return nil, err
	}
	return acquireLockAt(ctx, filepath.Join(taskDir, LockFileName))
}

func acquireLockAt(ctx context.Context, path string) (*Lock, error) {
	fd, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("tasks: open lock %q: %w", path, err)
	}
	if err := syscall.Flock(
		int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB,
	); err != nil {
		holder := readHolder(path)
		_ = fd.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, &LockedError{Holder: holder, cause: err}
		}
		return nil, fmt.Errorf("tasks: flock %q: %w", path, err)
	}
	host, _ := os.Hostname()
	holder := Holder{
		PID:       os.Getpid(),
		Host:      host,
		Phase:     PhaseFromContext(ctx),
		StartedAt: time.Now().UTC(),
	}
	if err := writeHolder(path, holder); err != nil {
		_ = syscall.Flock(int(fd.Fd()), syscall.LOCK_UN)
		_ = fd.Close()
		return nil, err
	}
	return &Lock{fd: fd, metaPath: path, holder: holder}, nil
}

// UpdatePhase rewrites the informational holder phase while keeping
// the kernel-level flock held.
func (l *Lock) UpdatePhase(phase string) error {
	if l == nil || l.fd == nil {
		return nil
	}
	l.holder.Phase = phase
	return writeHolder(l.metaPath, l.holder)
}

// Release drops the kernel flock and closes the fd. Idempotent and
// safe to call from a defer; subsequent calls are no-ops. Returns the
// first close error so callers that care can surface it; production
// code typically defers without checking.
func (l *Lock) Release() error {
	if l == nil || l.fd == nil {
		return nil
	}
	fd := l.fd
	l.fd = nil
	_ = syscall.Flock(int(fd.Fd()), syscall.LOCK_UN)
	return fd.Close()
}

// TryAcquireForReap is the reaper-facing non-blocking acquire. It
// returns (nil, nil) on contention so the reaper can treat "still
// held" as "still in flight, leave the row alone". On success the
// caller must Release immediately — TryAcquireForReap deliberately
// does not rewrite the metadata file (there is no real takeover) so
// the previous holder's last-known phase remains discoverable until a
// real AcquireLock supplants it.
func TryAcquireForReap(id string) (*Lock, error) {
	taskDir, err := EnsureDir(id)
	if err != nil {
		return nil, err
	}
	return tryAcquireForReapAt(filepath.Join(taskDir, LockFileName))
}

func tryAcquireForReapAt(path string) (*Lock, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("tasks: stat lock %q: %w", path, err)
	}
	fd, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("tasks: open lock %q: %w", path, err)
	}
	if err := syscall.Flock(
		int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB,
	); err != nil {
		_ = fd.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, fmt.Errorf("tasks: flock %q: %w", path, err)
	}
	return &Lock{fd: fd, metaPath: path, holder: readHolder(path)}, nil
}

func writeHolder(path string, h Holder) error {
	data, _ := toml.Marshal(h)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("tasks: write holder %q: %w", path, err)
	}
	return nil
}

// readHolder is best-effort: an empty / missing / malformed lock file
// returns a zero Holder rather than an error because the caller
// already knows acquisition failed and we do not want a stale meta
// file to mask the real EWOULDBLOCK.
func readHolder(path string) Holder {
	data, err := os.ReadFile(path)
	if err != nil {
		return Holder{}
	}
	var h Holder
	if err := toml.Unmarshal(data, &h); err != nil {
		return Holder{}
	}
	return h
}
