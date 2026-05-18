#!/bin/bash
# AC 9: The full test suite, lint checks, and line coverage gate pass.
set -euo pipefail
WT=/Users/fred.zhang/Projects/j/.j/worktrees/j-introduce-testify-assert-require-and-audit-the-e
cd "$WT"

echo "=== AC9: All gates pass ==="

FAILS=0

echo "--- make test ---"
if make test >/dev/null 2>&1; then
    echo "PASS: make test"
else
    echo "FAIL: make test"
    FAILS=$((FAILS+1))
fi

echo "--- make lint ---"
if make lint >/dev/null 2>&1; then
    echo "PASS: make lint (0 issues)"
else
    echo "FAIL: make lint"
    FAILS=$((FAILS+1))
fi

echo "--- make e2e ---"
if make e2e >/dev/null 2>&1; then
    echo "PASS: make e2e"
else
    echo "FAIL: make e2e"
    FAILS=$((FAILS+1))
fi

echo "--- make line-coverage ---"
if make line-coverage >/dev/null 2>&1; then
    echo "PASS: make line-coverage (all non-allowlisted symbols at 100%)"
else
    echo "FAIL: make line-coverage"
    FAILS=$((FAILS+1))
fi

echo "--- make lines ---"
if make lines >/dev/null 2>&1; then
    echo "PASS: make lines"
else
    echo "FAIL: make lines"
    FAILS=$((FAILS+1))
fi

if [ "$FAILS" -gt 0 ]; then
    echo "FAIL: $FAILS gate(s) failed"
    exit 1
fi

echo "PASS: AC9 satisfied - all gates pass"
