package testcases_test

import (
	"strings"
	"testing"

	"github.com/spacelions/j/internal/coding-agents/claude"
)

// TestFormatLog_TaskStartedRendersAsMarker verifies AC1: a claude
// stream-json line with type=system, subtype=task_started produces
// a single agentlog marker line that surfaces task_id, description,
// task_type, and the (possibly truncated) prompt.
func TestFormatLog_TaskStartedRendersAsMarker(t *testing.T) {
	t.Parallel()

	t.Run("all fields present", func(t *testing.T) {
		t.Parallel()
		in := []byte(
			`{"type":"system","subtype":"task_started",` +
				`"task_id":"tid-1","description":"explore codebase",` +
				`"task_type":"local_agent","prompt":"summarise the repo"}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		for _, want := range []string{
			"agent subtask_start",
			"task_id=tid-1",
			"description=explore codebase",
			"task_type=local_agent",
			"prompt=summarise the repo",
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

	t.Run("long prompt is truncated at 200 runes", func(t *testing.T) {
		t.Parallel()
		// Build a prompt that is 250 runes (all ASCII for simplicity).
		longPrompt := strings.Repeat("a", 250)
		in := []byte(
			`{"type":"system","subtype":"task_started",` +
				`"task_id":"tid-2","task_type":"local_agent",` +
				`"description":"d","prompt":"` + longPrompt + `"}` +
				"\n",
		)
		out := string(claude.New().FormatLog(in))

		if !strings.Contains(out, "prompt_chars=250") {
			t.Fatalf("expected prompt_chars=250 in output:\n%s", out)
		}
		if !strings.Contains(out, "…") {
			t.Fatalf("expected ellipsis in truncated output:\n%s", out)
		}
		if strings.Contains(out, longPrompt) {
			t.Fatalf("full prompt must not appear in output:\n%s", out)
		}
	})

	t.Run("single marker line emitted", func(t *testing.T) {
		t.Parallel()
		in := []byte(
			`{"type":"system","subtype":"task_started",` +
				`"task_id":"tid-3","description":"d",` +
				`"task_type":"local_agent","prompt":"p"}` +
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
