package testcases_test

// AC: Unknown structured Codex events and non-JSON output must be
// written to agent.log unchanged. This mirrors the existing
// agent_log_non_json_passthrough_test.go for the Codex backend.

import (
	"testing"

	"github.com/spacelions/j/internal/coding-agents/codex"
)

func TestCodexAgentLogFormatPassthrough(t *testing.T) {
	t.Parallel()
	a := codex.New()

	cases := []struct {
		name string
		in   []byte
	}{
		{
			name: "non-json",
			in: []byte(
				"panic: runtime error: index out of range\n",
			),
		},
		{
			name: "unknown-top-level-event",
			in: []byte(
				`{"type":"future.newthing","payload":42}` + "\n",
			),
		},
		{
			name: "unknown-item-type",
			in: []byte(
				`{"type":"item.completed","item":{` +
					`"type":"future_item","data":"x"}}` + "\n",
			),
		},
		{
			name: "malformed-json",
			in:   []byte(`{not json` + "\n"),
		},
		{
			name: "empty-line",
			in:   []byte("\n"),
		},
		{
			name: "binary-bytes",
			in:   []byte("\x00\xff\xfe mid-line\n"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := a.FormatLog(tc.in)
			if string(got) != string(tc.in) {
				t.Fatalf(
					"FormatLog(%q) = %q, want passthrough",
					tc.in, got,
				)
			}
		})
	}
}
