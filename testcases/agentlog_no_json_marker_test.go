package testcases_test

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/spacelions/j/internal/util/agentlog"
)

// markerHeader matches the new agent.log marker prefix:
// `<RFC3339Z>  <topic> <verb>` (verb optional). Pinning the on-the-
// wire shape at the package boundary catches any regression that re-
// introduces the legacy `>>> J ` sentinel + JSON payload.
var markerHeader = regexp.MustCompile(
	`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z  \S+( \S+)?`,
)

// TestAgentLog_SessionStart_NoJSONMarker pins the SPA-28 acceptance
// criterion: a `session_start` event must render as one
// human-readable line (RFC3339Z timestamp + `session start` topic+
// verb + sorted `k=v` fields) with no `>>> J ` sentinel and no JSON
// braces.
func TestAgentLog_SessionStart_NoJSONMarker(t *testing.T) {
	var buf bytes.Buffer
	err := agentlog.Emit(&buf, "session_start", map[string]any{
		"task":             "01KR",
		"orchestrator_pid": 4242,
		"hostname":         "ci.example",
		"cwd":              "/tmp/work",
		"phase":            "full",
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := buf.String()
	if strings.Contains(got, ">>> J ") {
		t.Fatalf("legacy sentinel re-introduced: %q", got)
	}
	if strings.Contains(got, "{") || strings.Contains(got, "}") {
		t.Fatalf("JSON payload re-introduced: %q", got)
	}
	trimmed := strings.TrimSuffix(got, "\n")
	if !markerHeader.MatchString(trimmed) {
		t.Fatalf("missing RFC3339Z+topic+verb prefix: %q", trimmed)
	}
	if !strings.Contains(trimmed, "session start") {
		t.Fatalf("missing `session start` topic+verb: %q", trimmed)
	}
	want := []string{
		"task=01KR",
		"orchestrator_pid=4242",
		"hostname=ci.example",
		"cwd=/tmp/work",
		"phase=full",
	}
	for _, kv := range want {
		if !strings.Contains(trimmed, kv) {
			t.Fatalf("missing field %q in %q", kv, trimmed)
		}
	}
}
