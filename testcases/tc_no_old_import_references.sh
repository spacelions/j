#!/bin/bash
# Test case: No old import path references remain
set -eu
WORKTREE="/Users/fred.zhang/Projects/j/.claude/worktrees/j-relocate-the-linear-integration-package-from-int"

echo "TC: No old import path references remain"

cd "$WORKTREE"

# Search Go files for old import path
if rg -l '"github.com/spacelions/j/internal/linear"' -g '*.go' | grep -q .; then
  echo "FAIL: old import path found"
  exit 1
fi

# Search source files for old directory path
if rg -l 'internal/linear' -g '*.go' -g '*.yml' -g '*.yaml' -g '*.md' -g '*.toml' -g '*.json' -g '*.sh' | grep -q .; then
  echo "FAIL: old directory path reference found"
  exit 1
fi

echo "PASS: No old import path or directory references found"
exit 0
