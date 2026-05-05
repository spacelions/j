package tasks

import (
	"io"
	"time"

	"github.com/spacelions/j/internal/util/agentlog"
)

// emitPhaseBegin writes a `phase_begin` marker to w. w is expected to
// be the same fd the orchestrator wired to the per-task agent.log
// (see internal/util/run/run.go SpawnIn): in the headless flow that
// makes the marker land alongside the human transcript; in the
// foreground flow it shows up on the user's terminal as a one-line
// summary, which is benign and grep-able via the agentlog.Sentinel.
//
// The helper swallows agentlog write errors. Markers are observability
// signal, not load-bearing data — losing one is strictly less harmful
// than aborting a phase begin.
func emitPhaseBegin(w io.Writer, phase string, t Task) {
	_ = agentlog.Emit(w, "phase_begin", map[string]any{
		"phase": phase,
		"task":  t.ID,
		"tool":  t.InvokedTool,
		"model": t.InvokedModel,
	})
}

// emitPhaseEnd writes a `phase_end` marker to w. duration_ms is
// computed from beginAt when set so the marker is self-contained;
// outcome is one of `done` / `help` / `pass` / `fail` per the
// agent.log marker convention.
func emitPhaseEnd(w io.Writer, phase string, beginAt *time.Time, t Task, outcome string) {
	fields := map[string]any{
		"phase":   phase,
		"task":    t.ID,
		"outcome": outcome,
	}
	if beginAt != nil {
		fields["duration_ms"] = time.Since(*beginAt).Milliseconds()
	}
	_ = agentlog.Emit(w, "phase_end", fields)
}
