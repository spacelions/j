#!/bin/bash
# Test case: Linear source files relocated to internal/tools/linear/
set -euo pipefail
WORKTREE="/Users/fred.zhang/Projects/j/.claude/worktrees/j-relocate-the-linear-integration-package-from-int"

echo "TC: Files relocated to internal/tools/linear/"
EXPECTED_FILES=(
  browser.go browser_test.go
  client.go client_test.go
  config.go config_test.go
  errors.go errors_test.go
  identifier.go identifier_test.go identifier_fuzz_test.go
  markdown.go markdown_test.go
  mutations.go mutations_test.go
  state_sync_types.go state_sync_types_test.go
  types.go
)

PASS=true
DEST="$WORKTREE/internal/tools/linear"
OLD="$WORKTREE/internal/linear"

if [ -d "$OLD" ]; then
  echo "FAIL: old internal/linear/ still exists"
  PASS=false
fi

if [ ! -d "$DEST" ]; then
  echo "FAIL: internal/tools/linear/ does not exist"
  PASS=false
fi

for f in "${EXPECTED_FILES[@]}"; do
  if [ ! -f "$DEST/$f" ]; then
    echo "FAIL: missing $f in $DEST"
    PASS=false
  fi
done

if [ "$PASS" = true ]; then
  echo "PASS: All 18 files present in internal/tools/linear/, old dir removed"
  exit 0
else
  exit 1
fi
