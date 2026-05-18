#!/bin/bash
# AC 2: testify/suite and testify/mock are not introduced.
set -euo pipefail
WT=/Users/fred.zhang/Projects/j/.j/worktrees/j-introduce-testify-assert-require-and-audit-the-e
cd "$WT"

echo "=== AC2: testify/suite and testify/mock are not introduced ==="

if grep -r 'testify/suite\|testify/mock' --include='*.go' . ; then
    echo "FAIL: testify/suite or testify/mock found in codebase"
    exit 1
fi

# Also check go.mod to ensure they're not dependencies
if grep -q 'testify/suite\|testify/mock' go.mod go.sum 2>/dev/null; then
    echo "FAIL: testify/suite or testify/mock found in go.mod or go.sum"
    exit 1
fi

echo "PASS: AC2 satisfied"
