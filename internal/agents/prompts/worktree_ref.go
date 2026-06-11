package prompts

// WorktreeRef names the git worktree a worker / verifier prompt
// targets. Branch is the persisted `Task.Worktree` slug (also the
// branch `gh pr list --head` matches); Path is the absolute,
// non-persisted checkout location `<task-dir>/worktree`. A zero
// Branch means "no worktree": the append helpers return the prompt
// unchanged so empty-worktree output stays byte-identical.
type WorktreeRef struct {
	Branch string
	Path   string
}
