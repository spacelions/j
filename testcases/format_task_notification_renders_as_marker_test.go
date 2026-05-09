//go:build !windows

package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/coding-agents/claude"
)

// TestFormatLog_TaskNotificationRendersAsMarker verifies AC3: a
// claude stream-json line with type=system,
// subtype=task_notification produces a single agentlog marker whose
// fields surface the sub-agent identifier, terminal status, summary
// (truncated when long), and the final tool_uses / total_tokens /
// duration_ms from usage.
func TestFormatLog_TaskNotificationRendersAsMarker(t *testing.T) {
	t.Parallel()

	t.Run("all fields surface", func(t *testing.T) {
		t.Parallel()
		in := []byte(
			`{"type":"system","subtype":"task_notification",` +
				`"task_id":"tid-1","status":"completed",` +
				`"summary":"task completed successfully",` +
				`"usage":{"total_tokens":200,"tool_uses":10,` +
				`"duration_ms":5000}}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		for _, want := range []string{
			"agent subtask_done",
			"task_id=tid-1",
			"status=completed",
			"summary=task completed successfully",
			"tool_uses=10",
			"total_tokens=200",
			"duration_ms=5000",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf(
					"missing %q in formatted output:\n%s", want, out,
				)
			}
		}
		if strings.Contains(out, `{"type"`) {
			t.Fatalf("raw JSON leaked into output:\n%s", out)
		}
	})

	t.Run("long summary truncated at 200 runes", func(t *testing.T) {
		t.Parallel()
		longSummary := strings.Repeat("c", 250)
		in := []byte(
			`{"type":"system","subtype":"task_notification",` +
				`"task_id":"tid-2","status":"completed",` +
				`"summary":"` + longSummary + `",` +
				`"usage":{"total_tokens":1,"tool_uses":1,` +
				`"duration_ms":1}}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		if !strings.Contains(out, "summary_chars=250") {
			t.Fatalf(
				"expected summary_chars=250 in output:\n%s", out,
			)
		}
		if strings.Contains(out, longSummary) {
			t.Fatalf(
				"full summary must not appear in output:\n%s", out,
			)
		}
	})
}
