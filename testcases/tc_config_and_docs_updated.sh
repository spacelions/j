#!/bin/bash
# Test case: config and documentation reflect new path
set -euo pipefail
WORKTREE="/Users/fred.zhang/Projects/j/.claude/worktrees/j-relocate-the-linear-integration-package-from-int"

echo "TC: Config and documentation updated"

cd "$WORKTREE"

PASS=true

# .golangci.yml should reference new path
if ! rg -q 'internal/tools/linear/client' .golangci.yml; then
  echo "FAIL: .golangci.yml missing tools/linear/client path"
  PASS=false
fi
if ! rg -q 'internal/tools/linear/browser' .golangci.yml; then
  echo "FAIL: .golangci.yml missing tools/linear/browser path"
  PASS=false
fi
echo "  .golangci.yml: $([ "$PASS" = true ] && echo PASS || echo FAIL)"

# Makefile should reference new path
if ! rg -q 'internal/tools/linear/browser' Makefile; then
  echo "FAIL: Makefile missing tools/linear/browser path"
  PASS=false
fi
if ! rg -q 'internal/tools/linear/client' Makefile; then
  echo "FAIL: Makefile missing tools/linear/client path"
  PASS=false
fi
if ! rg -q 'internal/tools/linear/config' Makefile; then
  echo "FAIL: Makefile missing tools/linear/config path"
  PASS=false
fi
echo "  Makefile: $([ "$PASS" = true ] && echo PASS || echo FAIL)"

# README.md should reference new path
if ! rg -q 'internal/tools/linear' README.md; then
  echo "FAIL: README.md missing tools/linear path"
  PASS=false
fi
echo "  README.md: $([ "$PASS" = true ] && echo PASS || echo FAIL)"

# Package name should remain linear
for f in "$WORKTREE"/internal/tools/linear/*.go; do
  pkg=$(head -50 "$f" | grep '^package ' | head -1 | awk '{print $2}')
  if [ "$pkg" != "linear" ]; then
    echo "FAIL: $(basename "$f") has package $pkg, expected linear"
    PASS=false
  fi
done
echo "  package names: $([ "$PASS" = true ] && echo PASS || echo FAIL)"

if [ "$PASS" = true ]; then
  echo "PASS: Config and documentation all updated correctly"
  exit 0
else
  exit 1
fi
