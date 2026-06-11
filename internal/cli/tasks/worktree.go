package tasks

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/spacelions/j/internal/cli/uitheme"
	"github.com/spacelions/j/internal/store"
	"github.com/spacelions/j/internal/store/tasks"
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
	for raw := range strings.SplitSeq(output, "\n") {
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
		if after, ok := strings.CutPrefix(line, "branch "); ok {
			cur.branch = strings.TrimSpace(after)
		}
	}
	flush()
	return records
}

// removeTaskWorktree runs `git worktree list --porcelain`, finds a
// worktree matching the task, then runs `git worktree remove
// --force` on that path. A record matches when its directory
// basename or checked-out branch equals the task's recorded name
// (legacy repo-root layouts), or when its path is the deterministic
// per-task checkout `<tasks-dir>/<id>/worktree` (current layout —
// every such checkout shares the basename "worktree", so only the
// path criterion can identify it). Paths are compared after
// filepath.EvalSymlinks on both sides because git prints
// symlink-resolved paths (darwin: /var vs /private/var). When
// t.Worktree is empty the name falls back to
// tasks.WorktreeNameFor(project, task) — the same deterministic slug
// legacy worker prompts used for `git worktree add`. Any git failure
// or ambiguity is reported as a single stderr warning without
// aborting the caller. A still-empty name after the fallback is a
// no-op.
func removeTaskWorktree(ctx context.Context, stderr io.Writer, t tasks.Task) {
	name := t.Worktree
	if name == "" {
		name = tasks.WorktreeNameFor(store.ProjectName(), t)
	}
	if name == "" {
		return
	}
	out, err := run.Output(ctx, "git", "worktree", "list", "--porcelain")
	if err != nil {
		uitheme.DangerousOutput(stderr, "J: worktree remove: %v", err)
		return
	}
	refsHead := "refs/heads/" + name
	taskPath := tasks.WorktreeDirFor(
		filepath.Join(tasks.DefaultDir(), t.ID),
	)
	var matches []worktreeRecord
	for _, rec := range parseWorktreeListPorcelain(out) {
		if filepath.Base(rec.path) == name || rec.branch == refsHead ||
			samePath(rec.path, taskPath) {
			matches = append(matches, rec)
		}
	}
	if len(matches) == 0 {
		return
	}
	if len(matches) > 1 {
		uitheme.DangerousOutput(stderr,
			"J: worktree remove: multiple worktrees matched %q; using %s",
			name, matches[0].path)
	}
	path := matches[0].path
	_, err = run.Output(ctx, "git", "worktree", "remove", "--force", path)
	if err != nil {
		uitheme.DangerousOutput(stderr, "J: worktree remove: %v", err)
	}
}

// samePath reports whether a and b name the same directory after
// resolving symlinks on BOTH sides: git prints symlink-resolved
// worktree paths while the task dir is derived from the cwd (darwin
// temp dirs are `/var/…` aliases of `/private/var/…`). A path that
// fails to resolve (e.g. already deleted) falls back to its literal
// form so the comparison degrades to a string match.
func samePath(a, b string) bool {
	return resolvePath(a) == resolvePath(b)
}

func resolvePath(p string) string {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return resolved
}
