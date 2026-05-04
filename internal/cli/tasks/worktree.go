package tasks

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/spacelions/j/internal/cli/banner"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/util/run"
)

type worktreeRecord struct {
	path   string
	branch string
}

// parseWorktreeListPorcelain parses `git worktree list --porcelain`
// output into (path, branch) pairs. Blank lines separate records;
// each record begins with a `worktree <path>` line and may include a
// `branch <ref>` line.
func parseWorktreeListPorcelain(output string) []worktreeRecord {
	var records []worktreeRecord
	var cur *worktreeRecord
	flush := func() {
		if cur != nil && cur.path != "" {
			records = append(records, *cur)
		}
		cur = nil
	}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			flush()
			p := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
			cur = &worktreeRecord{path: p}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(line, "branch ") {
			cur.branch = strings.TrimSpace(strings.TrimPrefix(line, "branch "))
		}
	}
	flush()
	return records
}

// removeTaskWorktree runs `git worktree list --porcelain`, finds a
// worktree whose directory basename or checked-out branch matches the
// task's recorded name, then runs `git worktree remove --force` on
// that path. When task.Worktree is empty (legacy rows that pre-date
// the persisted-worktree feature, or any row that never reached
// `j work`) the lookup falls back to store.WorktreeNameFor(project,
// task) — the same deterministic slug the worker prompt instructs the
// agent to use for `git worktree add`, so the on-disk basename is
// recoverable from project + summary alone. Any git failure or
// ambiguity is reported as a single stderr line prefixed `warning:
// worktree remove: ` without aborting the caller. A still-empty name
// after the fallback is a no-op.
func removeTaskWorktree(ctx context.Context, stderr io.Writer, task store.Task) {
	name := task.Worktree
	if name == "" {
		project, _ := store.ProjectName()
		name = store.WorktreeNameFor(project, task)
	}
	if name == "" {
		return
	}
	out, err := run.Output(ctx, "git", "worktree", "list", "--porcelain")
	if err != nil {
		banner.DangerousFprintf(stderr, "J: warning: worktree remove: %v\n", err)
		return
	}
	refsHead := "refs/heads/" + name
	var matches []worktreeRecord
	for _, rec := range parseWorktreeListPorcelain(out) {
		if filepath.Base(rec.path) == name || rec.branch == refsHead {
			matches = append(matches, rec)
		}
	}
	if len(matches) == 0 {
		return
	}
	if len(matches) > 1 {
		banner.DangerousFprintf(stderr, "J: warning: worktree remove: multiple worktrees matched %q; using %s\n", name, matches[0].path)
	}
	path := matches[0].path
	_, err = run.Output(ctx, "git", "worktree", "remove", "--force", path)
	if err != nil {
		banner.DangerousFprintf(stderr, "J: warning: worktree remove: %v\n", err)
	}
}
