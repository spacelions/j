package testcases_test

import (
	"testing"

	"github.com/spacelions/j/internal/coding-agents/deepseek"
)

// TestAgentLog_DeepSeek_IdentityFormatter verifies acceptance
// criterion: "Other backends (currently DeepSeek) continue to log
// whatever they log today, routed through the same code path." The
// deepseek FormatLog is the identity function — every byte the child
// wrote is preserved unchanged so the human-readable deepseek-tui
// trace reaches agent.log unmodified.
func TestAgentLog_DeepSeek_IdentityFormatter(t *testing.T) {
	t.Parallel()
	cases := [][]byte{
		[]byte("Thinking: checking if 17 is prime...\n"),
		[]byte(`{"type":"some_json"}`),
		[]byte(""),
		nil,
	}
	a := deepseek.New()
	for _, in := range cases {
		got := a.FormatLog(in)
		if string(got) != string(in) {
			t.Fatalf("FormatLog(%q) = %q, want identity", in, got)
		}
	}
}
