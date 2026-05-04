package store

import (
	"errors"
	"io"

	"github.com/spacelions/j/internal/cli/banner"
)

// PersistWarn opens `<cwd>/.j/tasks/list.db`, PutTask's the row, and
// closes the store. Path-resolve and put failures surface as a
// `warning: tasks ...` line on stderr; an open failure caused by the
// 2s file-lock timeout (typically because `j tasks` holds the
// lock) instead emits the refined `■ J: cannot write to database`
// line and the helper returns ErrOpenTimeout so callers can suppress
// any follow-up banner that would otherwise lie about the row being
// reachable. Persistence is best-effort by design so the phase
// lifecycle keeps running even when the row cannot be written.
// Designed to be called twice per phase run — once at begin, once at
// finish — so the bbolt file lock is never held across the agent
// invocation in between. Mirrors the inline open/close convention in
// PersistAgentSelection.
func PersistWarn(stderr io.Writer, task Task) error {
	path, err := DefaultTasksDBPath()
	if err != nil {
		banner.DangerousFprintf(stderr, "J: warning: tasks path: %v\n", err)
		return err
	}
	s, err := Open(path)
	if err != nil {
		if errors.Is(err, ErrOpenTimeout) {
			banner.CannotWriteToDatabase(stderr)
			return ErrOpenTimeout
		}
		banner.DangerousFprintf(stderr, "J: warning: tasks db: %v\n", err)
		return err
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		banner.DangerousFprintf(stderr, "J: warning: tasks put: %v\n", err)
		return err
	}
	return nil
}
