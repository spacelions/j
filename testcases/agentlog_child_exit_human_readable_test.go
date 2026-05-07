package testcases_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/util/agentlog"
)

// TestAgentLog_ChildExit_HumanReadable pins SPA-28's second emit site:
// `child_exit` must render as one human-readable line with the topic+
// verb `child exit` and the caller-supplied `name`, `pid`,
// `duration_ms`, `exit_code` fields as sorted `k=v` pairs — never as
// the legacy `>>> J ` JSON marker.
func TestAgentLog_ChildExit_HumanReadable(t *testing.T) {
	var buf bytes.Buffer
	err := agentlog.Emit(&buf, "child_exit", map[string]any{
		"name":        "claude",
		"pid":         99001,
		"duration_ms": int64(145215),
		"exit_code":   0,
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := buf.String()
	if strings.Contains(got, ">>> J ") {
		t.Fatalf("legacy sentinel re-introduced: %q", got)
	}
	if strings.Contains(got, "\"event\":") {
		t.Fatalf("JSON event key re-introduced: %q", got)
	}
	if !strings.Contains(got, "child exit") {
		t.Fatalf("missing `child exit` topic+verb: %q", got)
	}
	for _, kv := range []string{
		"name=claude",
		"pid=99001",
		"duration_ms=145215",
		"exit_code=0",
	} {
		if !strings.Contains(got, kv) {
			t.Fatalf("missing field %q in %q", kv, got)
		}
	}
	// Single line: exactly one trailing newline, no embedded newlines
	// between header and EOL.
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("expected one line, got %q", got)
	}
}
