package store

// This file used to host the lazy-create helpers (OpenDefault and
// OpenTaskLog) that materialised the .j layout on first call. Those
// helpers are gone: `j init` and the shared pre-flight are the only
// callers that write to disk now. The remaining helper here is the
// best-effort agent-selection persister used by both `j plan` and
// `j work`; renaming the file would generate churn without value, so
// the historical name is kept.

import (
	"fmt"
	"io"
	"strconv"
)

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
