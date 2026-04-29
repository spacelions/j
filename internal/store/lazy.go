package store

import (
	"fmt"
	"io"
	"strconv"
)

// OpenDefault resolves the default `<cwd>/.j/settings` DB path, opens
// it, and ensures the named bucket exists. Any failure is reported on
// stderr as a single "warning: ..." line and the function returns
// ok=false so callers can proceed without persistence (settings
// recording is intentionally optional for `j plan` and `j work`).
//
// This helper is shared by both subcommand packages so they cannot
// drift on the warning prefix or close-on-bucket-error semantics.
func OpenDefault(stderr io.Writer, bucket string) (*Store, bool) {
	path, err := DefaultPath()
	if err != nil {
		fmt.Fprintf(stderr, "warning: settings path: %v\n", err)
		return nil, false
	}
	s, err := Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: settings db: %v\n", err)
		return nil, false
	}
	if err := s.EnsureBucket(bucket); err != nil {
		fmt.Fprintf(stderr, "warning: settings bucket: %v\n", err)
		_ = s.Close()
		return nil, false
	}
	return s, true
}

// OpenTaskLog opens `<cwd>/.j/tasks` (a separate bbolt file from
// settings), ensures the bucket, and on failure reports one stderr
// line and returns ok=false so the caller can skip task logging the
// same way plan/work skip settings persistence.
func OpenTaskLog(stderr io.Writer, bucket string) (*Store, bool) {
	path, err := DefaultTasksPath()
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks path: %v\n", err)
		return nil, false
	}
	s, err := Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "warning: tasks db: %v\n", err)
		return nil, false
	}
	if err := s.EnsureBucket(bucket); err != nil {
		fmt.Fprintf(stderr, "warning: tasks bucket: %v\n", err)
		_ = s.Close()
		return nil, false
	}
	return s, true
}

// PersistAgentSelection writes tool/model/interactive into bucket as a
// best-effort operation: any error is logged to stderr as a single
// "warning: persist <key>: ..." line and the function returns. A nil
// store is a silent no-op so callers can pipe the value straight from
// Options.Store without nil-checks. Both `j plan` and `j work` use
// this so the on-disk schema is identical.
func PersistAgentSelection(s *Store, stderr io.Writer, bucket, tool, model string, interactive bool) {
	if s == nil {
		return
	}
	entries := [][2]string{
		{"tool", tool},
		{"model", model},
		{"interactive", strconv.FormatBool(interactive)},
	}
	for _, kv := range entries {
		if err := s.Put(bucket, kv[0], kv[1]); err != nil {
			fmt.Fprintf(stderr, "warning: persist %s: %v\n", kv[0], err)
			return
		}
	}
}
