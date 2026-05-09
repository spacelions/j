#!/bin/bash
# Test case: go build and go test for linear package pass
set -eu
WORKTREE="/Users/fred.zhang/Projects/j/.claude/worktrees/j-relocate-the-linear-integration-package-from-int"

echo "TC: go build and go test pass"

cd "$WORKTREE"

# Build
go build ./...
echo "  build: PASS"

# Test linear package
go test ./internal/tools/linear/... -count=1 -timeout 60s >/dev/null
echo "  linear tests: PASS"

# Test lifecycle (depends on linear)
go test ./internal/lifecycle/... -count=1 -timeout 120s >/dev/null
echo "  lifecycle tests: PASS"

# Test testcases
go test ./testcases/... -count=1 -timeout 300s >/dev/null
echo "  testcases: PASS"

# Verify test count unchanged
NEW_COUNT=$(go test -list '.*' ./internal/tools/linear/... 2>/dev/null | grep -c '^Test' || true)
if [ "$NEW_COUNT" != "62" ]; then
  echo "FAIL: test count changed (expected 62, got $NEW_COUNT)"
  exit 1
fi
echo "  test count unchanged ($NEW_COUNT): PASS"

echo "PASS: Build + tests succeed, test count unchanged"
exit 0
