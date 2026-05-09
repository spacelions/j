package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
	"github.com/spacelions/j/internal/coding-agents/deepseek"
)

// TestAgentLog_NonJSON_PassthroughVerbatim verifies acceptance
// criterion: "if the coding agent crashes or prints non-JSON output
// (a runtime panic, an error banner, anything the formatter does not
// recognise), that output still appears in agent.log verbatim. The
// formatter is best-effort and never silently drops content."
//
// Drives all three backends with a simulated panic line and confirms
// each formatter returns the input byte-for-byte unchanged.
func TestAgentLog_NonJSON_PassthroughVerbatim(t *testing.T) {
	t.Parallel()
	panicLine := []byte(
		"panic: runtime error: index out of range [3] with length 2\n")

	cases := []struct {
		name string
		fn   func([]byte) []byte
	}{
		{"claude", claude.New().FormatLog},
		{"cursor", cursor.New().FormatLog},
		{"deepseek", deepseek.New().FormatLog},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := c.fn(panicLine)
			if string(got) != string(panicLine) {
				t.Fatalf("%s: FormatLog(%q) = %q, want verbatim",
					c.name, panicLine, got)
			}
		})
	}
}
