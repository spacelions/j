# git-hooks

In-repo git hooks. The local repo activates them by pointing
`core.hooksPath` here.

## What `pre-commit` does

1. Lists staged files (`git diff --cached --name-only --diff-filter=ACMR`).
2. Skips `*_test.go` and anything under `vendor/`, `bin/`, `testcases/`,
   or `.j/`.
3. For each remaining staged file, reads the staged blob and fails the
   commit if it has more than 300 lines, naming each offender.
4. If the line check passes, runs `make test`. The commit is aborted on
   any non-zero exit.

Existing files that are already over 300 lines do not block commits
unless they are part of the staged set.

## Install

From the repo root:

```sh
make install-hooks
```

That sets `core.hooksPath` to `scripts/git-hooks` for this clone.
Re-running it is safe.
