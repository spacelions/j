package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/coding-agents/claude"
)

// TestAgentLog_MultiBlockOrdering verifies that an assistant turn
// containing multiple content blocks (thinking, then text, then
// tool_use) renders as one agentlog marker line per block in the
// same order they appeared in the source event — acceptance criterion:
// "one line per part, in the order they were produced."
func TestAgentLog_MultiBlockOrdering(t *testing.T) {
	t.Parallel()
	src := []byte(`{"type":"assistant","message":{"content":[` +
		`{"type":"thinking","thinking":"let me reason"},` +
		`{"type":"text","text":"here is the answer"},` +
		`{"type":"tool_use","name":"Bash","input":{"cmd":"ls"}}` +
		`]}}` + "\n")

	got := string(claude.New().FormatLog(src))

	thinkAt := strings.Index(got, "agent thinking")
	msgAt := strings.Index(got, "agent message")
	toolAt := strings.Index(got, "agent tool_use")

	if thinkAt < 0 {
		t.Fatalf("missing agent thinking line: %q", got)
	}
	if msgAt < 0 {
		t.Fatalf("missing agent message line: %q", got)
	}
	if toolAt < 0 {
		t.Fatalf("missing agent tool_use line: %q", got)
	}
	if thinkAt >= msgAt || msgAt >= toolAt {
		t.Fatalf(
			"wrong order: thinking=%d message=%d tool_use=%d in %q",
			thinkAt, msgAt, toolAt, got)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), got)
	}
}
