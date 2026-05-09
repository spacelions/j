//go:build !windows

package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/coding-agents/claude"
)

// TestFormatLog_UnknownSystemSubtypePassthrough verifies AC5: a
// claude stream-json system envelope with an unrecognised subtype
// falls through verbatim so that future event shapes remain visible
// in agent.log without requiring a code change.
func TestFormatLog_UnknownSystemSubtypePassthrough(t *testing.T) {
	t.Parallel()

	unknownSubtypes := []string{"warning", "debug", "future_type_xyz"}
	for _, sub := range unknownSubtypes {
		t.Run(sub, func(t *testing.T) {
			t.Parallel()
			raw := `{"type":"system","subtype":"` + sub +
				`","text":"some data"}` + "\n"
			in := []byte(raw)
			out := string(claude.New().FormatLog(in))

			if out != raw {
				t.Fatalf(
					"subtype=%q: expected raw passthrough\n"+
						"got:  %q\nwant: %q",
					sub, out, raw,
				)
			}
			// Must NOT have been converted to a marker line.
			if strings.Contains(out, "agent ") {
				t.Fatalf(
					"subtype=%q: unexpected marker in output: %q",
					sub, out,
				)
			}
		})
	}
}
