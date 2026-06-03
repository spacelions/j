package codingagents

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/spacelions/j/internal/util/run"
)

// watcherLivenessInterval is the cadence at which the shared watch
// loop re-scans capture.TaskDir while idle. It bounds the loop's
// worst-case latency to react to events the filesystem watcher
// missed — including backends that exit without ever writing the
// session_meta record the scan looks for.
const watcherLivenessInterval = 200 * time.Millisecond

// watchResumeID is the shared core both the foreground and
// background watchers drive. It creates the fsnotify watcher,
// registers directory watches under capture.TaskDir, scans via
// capturer on every event and on every liveness tick, and returns
// when capturer resolves a non-empty id, when shouldStop returns
// true, or when ctx is cancelled. shouldStop is consulted only on
// the liveness tick so foreground callers can pass a nil-equivalent
// "never stop" predicate without paying for per-event work.
func watchResumeID(
	ctx context.Context,
	capturer ResumeIDCapturer,
	capture ResumeCapture,
	shouldStop func() bool,
) string {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return ""
	}
	defer func() { _ = w.Close() }()
	addDirWatches(w, capture.TaskDir)
	ticker := time.NewTicker(watcherLivenessInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ""
		case <-ticker.C:
			id, _ := capturer.CaptureResumeID(
				ctx, capture.TaskDir, capture.Since,
			)
			if id != "" {
				return id
			}
			if shouldStop != nil && shouldStop() {
				return ""
			}
		case ev := <-w.Events:
			maybeAddDir(w, ev)
			id, _ := capturer.CaptureResumeID(
				ctx, capture.TaskDir, capture.Since,
			)
			if id != "" {
				return id
			}
		}
	}
}

// WatchBackgroundResumeID blocks until capturer resolves a non-empty
// resume id under capture.TaskDir, the worker pid disappears, or ctx
// is cancelled. Used by the detached/headless code path where the
// watcher must release its goroutine when the spawned backend exits
// without producing session metadata.
func WatchBackgroundResumeID(
	ctx context.Context,
	capturer ResumeIDCapturer,
	capture ResumeCapture,
	pid int,
) string {
	return watchResumeID(ctx, capturer, capture, func() bool {
		return !run.IsAlive(pid)
	})
}

// WatchForegroundResumeID blocks until capturer resolves a non-empty
// resume id or ctx is cancelled. Used by the interactive/TUI code
// path: the foreground TUI keeps the backend in this process tree so
// there is no pid to poll, and the caller cancels ctx after the TUI
// returns.
func WatchForegroundResumeID(
	ctx context.Context,
	capturer ResumeIDCapturer,
	capture ResumeCapture,
) string {
	return watchResumeID(ctx, capturer, capture, nil)
}

// addDirWatches walks root and registers a watch on every directory.
// Walk and Add errors are swallowed: a directory we cannot watch is
// effectively invisible to the loop, which the liveness ticker plus
// the caller's pre-watch scan still cover.
func addDirWatches(w *fsnotify.Watcher, root string) {
	_ = filepath.WalkDir(
		root,
		func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				//nolint:nilerr // best-effort walk; an
				// unreadable subtree is invisible to fsnotify
				// but the caller's pre-watch scan and
				// liveness ticker still guarantee progress.
				return nil
			}
			if d.IsDir() {
				_ = w.Add(path)
			}
			return nil
		},
	)
}

// maybeAddDir extends the watch onto a newly created subdirectory so
// rollouts written into a freshly minted dated folder still trigger
// the loop. Non-Create events and non-directory targets are ignored.
func maybeAddDir(w *fsnotify.Watcher, ev fsnotify.Event) {
	if !ev.Op.Has(fsnotify.Create) {
		return
	}
	info, err := os.Stat(ev.Name)
	if err != nil || !info.IsDir() {
		return
	}
	_ = w.Add(ev.Name)
}
