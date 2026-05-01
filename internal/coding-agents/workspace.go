package codingagents

import (
	"os"
	"path/filepath"
)

// DefaultWorkspace returns a meaningful workspace path for the given
// markdown target: the directory that holds it. plan.md is written there
// too, so it is a self-contained working area for any Agent backend.
// Centralising this lets the rule evolve (for example to derive a
// worktree name from the target) in one place.
func DefaultWorkspace(targetPath string) string {
	return filepath.Dir(targetPath)
}

// ProjectRootWorkspace returns the current working directory. `j
// verify` uses it so cursor-agent is invoked with
// `--workspace <project-root>` (not inside .j/tasks/<id>/): the
// verifier then resolves the target worktree itself via
// `git worktree list` rather than having the orchestrator chdir
// around. Plan / Work still use DefaultWorkspace because those
// flows want the self-contained per-task folder.
//
// The helper intentionally does not return an error: the only
// failure mode is os.Getwd (e.g. the cwd was removed while the
// process is running) and that case yields an empty string which
// the downstream agent runner surfaces when it rejects the empty
// `--workspace` argument. Hiding the error here keeps the single
// call site in Agent.Verify free of a defensive branch that tests
// cannot exercise on darwin.
func ProjectRootWorkspace() string {
	cwd, _ := os.Getwd()
	return cwd
}
