package store

import (
	"io"

	"github.com/spacelions/j/internal/cli/banner"
)

// PersistWarn opens a tasks-mode store at `<cwd>/.j/tasks` and writes
// the row via PutTask. Path-resolve and put failures surface as a
// single `warning: tasks ...` line on stderr; the helper returns
// silently — persistence is best-effort by design so the phase
// lifecycle keeps running even when the row cannot be written. With
// per-task TOML files there is no shared lock to contend with, so
// the open-timeout class of failure no longer reaches this path.
// Designed to be called twice per phase run — once at begin, once at
// finish — so the original bbolt-era convention of "release the lock
// between writes" stays intact even though there is no longer a lock
// to release.
func PersistWarn(stderr io.Writer, task Task) {
	tasksDir, err := DefaultTasksDir()
	if err != nil {
		banner.DangerousFprintf(stderr, "J: warning: tasks path: %v\n", err)
		return
	}
	s := OpenTasks(tasksDir)
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		banner.DangerousFprintf(stderr, "J: warning: tasks put: %v\n", err)
	}
}
