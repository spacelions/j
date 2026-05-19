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

// watcherLivenessInterval is the cadence at which WatchActiveResumeID
// re-checks the worker pid while idle. It bounds the loop's worst-case
// latency to react to a process exit that produced no relevant
// filesystem events (e.g. the backend crashed before writing its
// session_meta record).
const watcherLivenessInterval = 200 * time.Millisecond

// WatchActiveResumeID blocks until capturer resolves a non-empty
// resume id under capture.TaskDir, the worker pid disappears, or ctx
// is cancelled. Filesystem events drive scans through capturer; a
// liveness ticker covers backends that exit without writing the
// session_meta the scan looks for. Returns the captured id, or "" if
// the loop ended without one.
func WatchActiveResumeID(
	ctx context.Context,
	capturer ResumeIDCapturer,
	capture ResumeCapture,
	pid int,
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
			if !run.IsAlive(pid) {
				return ""
			}
		case ev, ok := <-w.Events:
			if !ok {
				return ""
			}
			maybeAddDir(w, ev)
			id, _ := capturer.CaptureResumeID(
				ctx, capture.TaskDir, capture.Since,
			)
			if id != "" {
				return id
			}
		case <-w.Errors:
		}
	}
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
