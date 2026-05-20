package codingagents

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddDirWatches_MissingRoot covers the WalkDir error path: a
// non-existent root surfaces a non-nil err in the walk callback,
// which the helper deliberately swallows so the watcher keeps
// running with whatever subset it managed to register.
func TestAddDirWatches_MissingRoot(t *testing.T) {
	w, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	addDirWatches(w, filepath.Join(t.TempDir(), "missing"))
	assert.Empty(t, w.WatchList())
}

// TestMaybeAddDir_CreateDirRegistersWatch pins the happy path:
// a Create event on a real subdirectory extends the watch onto it.
func TestMaybeAddDir_CreateDirRegistersWatch(t *testing.T) {
	w, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	sub := filepath.Join(t.TempDir(), "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))
	maybeAddDir(w, fsnotify.Event{Name: sub, Op: fsnotify.Create})
	assert.Contains(t, w.WatchList(), sub)
}

// TestMaybeAddDir_NonCreateIgnored pins the early-return: a Write
// event (or anything else without the Create bit) must not extend
// the watch.
func TestMaybeAddDir_NonCreateIgnored(t *testing.T) {
	w, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	sub := filepath.Join(t.TempDir(), "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))
	maybeAddDir(w, fsnotify.Event{Name: sub, Op: fsnotify.Write})
	assert.Empty(t, w.WatchList())
}

// TestMaybeAddDir_CreateFileIgnored pins the !IsDir branch:
// rollout files themselves trigger Create events whose targets are
// regular files; the helper must not try to add a watch to them.
func TestMaybeAddDir_CreateFileIgnored(t *testing.T) {
	w, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	file := filepath.Join(t.TempDir(), "rollout.jsonl")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	maybeAddDir(w, fsnotify.Event{Name: file, Op: fsnotify.Create})
	assert.Empty(t, w.WatchList())
}

// TestWatchActiveResumeID_NewWatcherFails forces fsnotify.NewWatcher
// to fail by squeezing RLIMIT_NOFILE down below the file-descriptor
// budget the kernel needs to allocate the inotify/kqueue instance.
// The watcher's fast-path return ("") is exercised; rlimit is
// restored immediately after the call so subsequent test-framework
// allocations succeed.
func TestWatchActiveResumeID_NewWatcherFails(t *testing.T) {
	dir := t.TempDir()
	var orig syscall.Rlimit
	require.NoError(
		t, syscall.Getrlimit(syscall.RLIMIT_NOFILE, &orig),
	)
	low := orig
	low.Cur = 4
	require.NoError(
		t, syscall.Setrlimit(syscall.RLIMIT_NOFILE, &low),
	)
	t.Cleanup(func() {
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &orig)
	})
	got := WatchActiveResumeID(
		t.Context(),
		capturingAgent{},
		ResumeCapture{TaskDir: dir, Stderr: &bytes.Buffer{}},
		os.Getpid(),
	)
	_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &orig)
	assert.Empty(t, got)
}
