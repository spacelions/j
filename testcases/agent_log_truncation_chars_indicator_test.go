package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/coding-agents/claude"
	"github.com/spacelions/j/internal/coding-agents/cursor"
)

// TestAgentLog_Truncation_CharsIndicator verifies acceptance criterion:
// "when an event carries long content the line is truncated to a
// scannable length and includes a length indicator so the reader knows
// content was elided."
//
// Drives both claude and cursor backends with text longer than the
// 200-rune cap and checks that each rendered line contains the `…`
// ellipsis and a `chars=<n>` field.
func TestAgentLog_Truncation_CharsIndicator(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 250)

	tests := []struct {
		name string
		src  []byte
	}{
		{
			name: "claude assistant text",
			src: []byte(`{"type":"assistant","message":{"content":` +
				`[{"type":"text","text":"` + long + `"}]}}` + "\n"),
		},
		{
			name: "claude thinking block",
			src: []byte(`{"type":"assistant","message":{"content":` +
				`[{"type":"thinking","thinking":"` + long + `"}]}}` +
				"\n"),
		},
		{
			name: "cursor assistant text",
			src: []byte(`{"type":"assistant","message":{"content":` +
				`[{"type":"text","text":"` + long + `"}]}}` + "\n"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got string
			if strings.HasPrefix(tc.name, "claude") {
				got = string(claude.New().FormatLog(tc.src))
			} else {
				got = string(cursor.New().FormatLog(tc.src))
			}
			if !strings.Contains(got, "chars=250") {
				t.Fatalf("missing chars=250 in %q", got)
			}
			if !strings.Contains(got, "…") {
				t.Fatalf("missing ellipsis in %q", got)
			}
			if strings.Contains(got, long) {
				t.Fatalf("untruncated body leaked into log: %q", got)
			}
		})
	}
}
