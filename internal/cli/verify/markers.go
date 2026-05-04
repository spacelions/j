package verify

import (
	"io"

	"github.com/spacelions/j/internal/util/agentlog"
)

// emitIterationBegin writes one `verify_iteration_begin` marker so a
// tailer can pin which loop turn the verifier is on without reading
// bbolt. Errors are swallowed: markers are observability signal.
func emitIterationBegin(w io.Writer, taskID string, iteration, max int) {
	_ = agentlog.Emit(w, "verify_iteration_begin", map[string]any{
		"task":           taskID,
		"iteration":      iteration,
		"max_iterations": max,
	})
}

// emitVerdict writes one `verdict` marker carrying the parsed
// PASS/FAIL plus the findings path so a tailer can correlate with the
// verifier's prose without re-reading verifier_findings.md.
func emitVerdict(w io.Writer, taskID string, iteration int, verdict, findingsPath string) {
	_ = agentlog.Emit(w, "verdict", map[string]any{
		"task":          taskID,
		"iteration":     iteration,
		"verdict":       verdict,
		"findings_path": findingsPath,
	})
}

// emitIterationEnd closes the iteration_begin/end pairing so a tailer
// sees one matching pair per loop turn even when the verdict short
// circuits the loop early.
func emitIterationEnd(w io.Writer, taskID string, iteration int, verdict string) {
	_ = agentlog.Emit(w, "verify_iteration_end", map[string]any{
		"task":      taskID,
		"iteration": iteration,
		"verdict":   verdict,
	})
}
