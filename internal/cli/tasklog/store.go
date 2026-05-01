package tasklog

import (
	"fmt"
	"io"

	"github.com/spacelions/j/internal/store"
)

// OpenTaskLog opens `<cwd>/.j/tasks/list.db` and returns the store
// together with a success flag. It is the post-init replacement for
// store.OpenTaskLog: pre-flight (`j init`) has already laid the
// layout down, so any failure here surfaces as a single
// "warning: ..." line on stderr and the caller should short-circuit
// without panicking. Callers that just want the open-write-close
// pattern should use PersistWarn instead; both helpers share the
// same shape so a future consolidation does not break callers.
//
// The bbolt handle is owned by the caller after a successful open;
// PersistWarn closes the store inside the call so the file lock is
// not held across long-running agent invocations.
func OpenTaskLog(stderr io.Writer) (*store.Store, bool) {
	path, err := store.DefaultTasksDBPath()
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks path: %v\n", err)
		return nil, false
	}
	s, err := store.Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks db: %v\n", err)
		return nil, false
	}
	return s, true
}

// PersistWarn opens the task log, PutTask's the row, and closes the
// store. Open and put failures each surface as a single
// `warning: ...` line on stderr (open via OpenTaskLog, put inline)
// and the helper returns; persistence is best-effort by design so
// the phase lifecycle keeps running even when the row cannot be
// written. Designed to be called twice per phase run — once at
// begin, once at finish — so the bbolt file lock is never held
// across the agent invocation in between.
func PersistWarn(stderr io.Writer, task store.Task) {
	s, ok := OpenTaskLog(stderr)
	if !ok {
		return
	}
	defer func() { _ = s.Close() }()
	if err := s.PutTask(task); err != nil {
		fmt.Fprintf(stderr, "warning: tasks put: %v\n", err)
	}
}
