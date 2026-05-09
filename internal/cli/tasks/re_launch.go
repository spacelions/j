package tasks

import (
	"context"
	"io"

	"github.com/spacelions/j/internal/cli/uitheme"
)

// launchOptions bundles the shared run-orchestrator parameters that
// re-plan, re-work, and re-verify all need. Each command builds the
// argv vector and per-task agent log path itself; this helper covers
// the inline-vs-detached fork plus the row-stamping side effect so
// the per-command RunE stays under the cyclomatic-complexity budget.
type launchOptions struct {
	taskID       string
	jBinary      string
	args         []string
	agentLogPath string
	interactive  bool
	stdout       io.Writer
	stderr       io.Writer
}

// launchOrchestrator runs the orchestrate child either inline (parent
// stdin/stdout/stderr inherited so a TUI can render) or detached
// (fire-and-forget child whose output lands in the per-task agent log).
// The row's AgentLogPath is stamped via stampSpawnOnRow on both
// branches so `j tasks` can surface the latest log path.
func launchOrchestrator(
	ctx context.Context, lo launchOptions,
) error {
	if lo.interactive {
		stampSpawnOnRow(lo.stderr, lo.taskID, "")
		return runInlineOrchestrator(ctx, lo.jBinary, lo.args)
	}
	pid, err := spawnDetachedOrchestrator(
		ctx, lo.jBinary, lo.agentLogPath, lo.args)
	if err != nil {
		return err
	}
	stampSpawnOnRow(lo.stderr, lo.taskID, lo.agentLogPath)
	uitheme.NormalForkDialog(
		lo.stdout, "task "+lo.taskID, pid, lo.agentLogPath)
	return nil
}
