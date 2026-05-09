//go:build !windows

package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/coding-agents/claude"
)

// TestFormatLog_StatusRendersAsMarker verifies AC4: a claude
// stream-json line with type=system, subtype=status produces a
// single agentlog marker whose only field is status; the noisy
// uuid and session_id fields must not appear.
func TestFormatLog_StatusRendersAsMarker(t *testing.T) {
	t.Parallel()

	t.Run("status field surfaces", func(t *testing.T) {
		t.Parallel()
		in := []byte(
			`{"type":"system","subtype":"status",` +
				`"status":"requesting",` +
				`"uuid":"u-123","session_id":"s-456"}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		if !strings.Contains(out, "agent status") {
			t.Fatalf("missing marker header in output:\n%s", out)
		}
		if !strings.Contains(out, "status=requesting") {
			t.Fatalf(
				"missing status=requesting in output:\n%s", out,
			)
		}
	})

	t.Run("uuid and session_id are not rendered", func(t *testing.T) {
		t.Parallel()
		in := []byte(
			`{"type":"system","subtype":"status",` +
				`"status":"requesting",` +
				`"uuid":"u-123","session_id":"s-456"}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		for _, noise := range []string{"uuid=", "session_id="} {
			if strings.Contains(out, noise) {
				t.Fatalf(
					"noise field %q must not appear in output:\n%s",
					noise, out,
				)
			}
		}
	})

	t.Run("single marker line emitted", func(t *testing.T) {
		t.Parallel()
		in := []byte(
			`{"type":"system","subtype":"status","status":"ready"}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != 1 {
			t.Fatalf(
				"expected 1 output line, got %d:\n%s", len(lines), out,
			)
		}
	})
}
