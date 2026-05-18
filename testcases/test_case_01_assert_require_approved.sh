#!/bin/bash
# AC 1: testify/assert and testify/require are approved for tests.
set -euo pipefail
WT=/Users/fred.zhang/Projects/j/.j/worktrees/j-introduce-testify-assert-require-and-audit-the-e
cd "$WT"

echo "=== AC1: testify/assert and testify/require are approved for tests ==="

# Check that assert and require appear in test files
imports=$(grep -rl 'testify/assert\|testify/require' --include='*_test.go' .)
if [ -z "$imports" ]; then
    echo "FAIL: no test files import testify/assert or testify/require"
    exit 1
fi
echo "Found testify imports in:"
echo "$imports"

# Check that testify is a direct dependency in go.mod (not indirect)
if grep -q 'github.com/stretchr/testify v[0-9]' go.mod && ! grep -q 'github.com/stretchr/testify .* // indirect$' go.mod; then
    echo "PASS: testify is a direct dependency in go.mod"
else
    echo "FAIL: testify is not a direct dependency"
    exit 1
fi

echo "PASS: AC1 satisfied"
