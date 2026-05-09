package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/coding-agents/claude"
)

// TestFormatLog_TaskProgressRendersAsMarker verifies AC2: a claude
// stream-json line with type=system, subtype=task_progress produces
// a single agentlog marker whose fields surface tool_uses,
// total_tokens, duration_ms, and last_tool_name; zero usage fields
// are omitted; and the description is truncated when long.
func TestFormatLog_TaskProgressRendersAsMarker(t *testing.T) {
	t.Parallel()

	t.Run("all usage fields surface", func(t *testing.T) {
		t.Parallel()
		in := []byte(
			`{"type":"system","subtype":"task_progress",` +
				`"task_id":"tid-1","description":"reading file",` +
				`"last_tool_name":"Read",` +
				`"usage":{"total_tokens":500,"tool_uses":5,` +
				`"duration_ms":3000}}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		for _, want := range []string{
			"agent subtask_progress",
			"task_id=tid-1",
			"description=reading file",
			"last_tool_name=Read",
			"tool_uses=5",
			"total_tokens=500",
			"duration_ms=3000",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf(
					"missing %q in formatted output:\n%s", want, out,
				)
			}
		}
	})

	t.Run("zero usage values are omitted", func(t *testing.T) {
		t.Parallel()
		// No usage block → all zero; must not appear as `key=0`.
		in := []byte(
			`{"type":"system","subtype":"task_progress",` +
				`"task_id":"tid-2","description":"early step"}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		for _, leak := range []string{
			"tool_uses=0", "total_tokens=0", "duration_ms=0",
		} {
			if strings.Contains(out, leak) {
				t.Fatalf(
					"zero field %q must not appear in output:\n%s",
					leak, out,
				)
			}
		}
		if !strings.Contains(out, "agent subtask_progress") {
			t.Fatalf("marker header missing:\n%s", out)
		}
	})

	t.Run("long description truncated at 200 runes", func(t *testing.T) {
		t.Parallel()
		longDesc := strings.Repeat("b", 250)
		in := []byte(
			`{"type":"system","subtype":"task_progress",` +
				`"task_id":"tid-3","description":"` + longDesc + `",` +
				`"usage":{"total_tokens":1,"tool_uses":1,` +
				`"duration_ms":100}}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		if !strings.Contains(out, "description_chars=250") {
			t.Fatalf(
				"expected description_chars=250 in output:\n%s", out,
			)
		}
		if strings.Contains(out, longDesc) {
			t.Fatalf(
				"full description must not appear in output:\n%s", out,
			)
		}
	})
}
