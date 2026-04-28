package codingagents

import "path/filepath"

// DefaultWorkspace returns a meaningful workspace path for the given
// markdown target: the directory that holds it. plan.md is written there
// too, so it is a self-contained working area for any Agent backend.
// Centralising this lets the rule evolve (for example to derive a
// worktree name from the target) in one place.
func DefaultWorkspace(targetPath string) string {
	return filepath.Dir(targetPath)
}
