package tasks

import (
	"time"

	"github.com/spacelions/j/internal/util/agentlog"
)

// emitPhaseBegin appends a `phase_begin` marker to the per-task
// `agent.log` at agentLogPath. Markers never land on the user's
// terminal: an empty agentLogPath is a silent no-op (resume / test
// paths and pre-existing rows that have no agent.log destination)
// and any non-empty path is opened, appended to, and closed by
// agentlog.EmitTo.
//
// The helper swallows agentlog write errors. Markers are observability
// signal, not load-bearing data — losing one is strictly less harmful
// than aborting a phase begin.
func emitPhaseBegin(agentLogPath, phase string, t Task) {
	tool, model := t.DisplayToolModel()
	_ = agentlog.EmitTo(agentLogPath, "phase_begin", map[string]any{
		"phase": phase,
		"task":  t.ID,
		"tool":  tool,
		"model": model,
	})
}

// emitPhaseEnd appends a `phase_end` marker to agentLogPath.
// duration_ms is computed from beginAt when non-zero so the marker is
// self-contained; outcome is one of `done` / `help` / `pass` / `fail`
// per the agent.log marker convention. An empty agentLogPath is a
// silent no-op.
func emitPhaseEnd(agentLogPath, phase string, beginAt time.Time, t Task, outcome string) {
	fields := map[string]any{
		"phase":   phase,
		"task":    t.ID,
		"outcome": outcome,
	}
	if !beginAt.IsZero() {
		fields["duration_ms"] = time.Since(beginAt).Milliseconds()
	}
	_ = agentlog.EmitTo(agentLogPath, "phase_end", fields)
}

func emitVerifyIterationBegin(agentLogPath, taskID string, iteration, max int) {
	_ = agentlog.EmitTo(agentLogPath, "verify_iteration_begin", map[string]any{
		"task":           taskID,
		"iteration":      iteration,
		"max_iterations": max,
	})
}

func emitVerdict(agentLogPath, taskID string, iteration int, verdict, findingsPath string) {
	_ = agentlog.EmitTo(agentLogPath, "verdict", map[string]any{
		"task":          taskID,
		"iteration":     iteration,
		"verdict":       verdict,
		"findings_path": findingsPath,
	})
}

func emitVerifyIterationEnd(agentLogPath, taskID string, iteration int, verdict string) {
	_ = agentlog.EmitTo(agentLogPath, "verify_iteration_end", map[string]any{
		"task":      taskID,
		"iteration": iteration,
		"verdict":   verdict,
	})
}
