package store

import (
	"io"

	"github.com/spacelions/j/internal/cli/banner"
)

// PersistWarn opens `<cwd>/.j/tasks/list.db`, PutTask's the row, and
// closes the store. Path-resolve, open, and put failures each surface
// as a single `warning: tasks ...` line on stderr and the helper
// returns; persistence is best-effort by design so the phase
// lifecycle keeps running even when the row cannot be written.
// Designed to be called twice per phase run — once at begin, once at
// finish — so the bbolt file lock is never held across the agent
// invocation in between. Mirrors the inline open/close convention in
// PersistAgentSelection.
func PersistWarn(stderr io.Writer, task Task) {
	path, err := DefaultTasksDBPath()
	if err != nil {
		banner.DangerousFprintf(stderr, "J: warning: tasks path: %v\n", err)
		return
	}
	s, err := Open(path)
	if err != nil {
		banner.DangerousFprintf(stderr, "J: warning: tasks db: %v\n", err)
		return
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		banner.DangerousFprintf(stderr, "J: warning: tasks put: %v\n", err)
	}
}
