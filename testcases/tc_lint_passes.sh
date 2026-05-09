#!/bin/bash
# Test case: golangci-lint passes with updated suppressions
set -euo pipefail
WORKTREE="/Users/fred.zhang/Projects/j/.claude/worktrees/j-relocate-the-linear-integration-package-from-int"

echo "TC: golangci-lint passes"

cd "$WORKTREE"

if ! go tool golangci-lint run ./... 2>&1; then
  echo "FAIL: golangci-lint reported issues"
  exit 1
fi
echo "PASS: golangci-lint reports 0 issues"
exit 0
