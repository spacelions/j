#!/bin/bash
# AC 6: Domain-specific failure messages are preserved when they explain
#        behavior better than a generic assertion failure.
set -euo pipefail
WT=/Users/fred.zhang/Projects/j/.j/worktrees/j-introduce-testify-assert-require-and-audit-the-e
cd "$WT"

echo "=== AC6: Domain-specific failure messages preserved ==="

FAILS=0

# Check that custom messages remain in the converted tests
# Example: "bare j settings would fail" in settings/cmd_test.go
if grep -q 'bare .j settings. would fail' internal/cli/settings/cmd_test.go; then
    echo "PASS: custom message 'bare j settings would fail' preserved"
else
    echo "FAIL: custom message lost in settings/cmd_test.go"
    FAILS=$((FAILS+1))
fi

# Check: "missing set subcommand" / "missing reset subcommand"
if grep -q 'missing.*subcommand' internal/cli/settings/cmd_test.go; then
    echo "PASS: subcommand failure messages preserved with context"
else
    echo "FAIL: subcommand failure messages lost"
    FAILS=$((FAILS+1))
fi

# Check: "decode body" message with body content preserved in linear_state_sync_test.go
if grep -q 'decode body' internal/lifecycle/linear_state_sync_test.go; then
    echo "PASS: 'decode body' context message preserved"
else
    echo "FAIL: 'decode body' message lost"
    FAILS=$((FAILS+1))
fi

# Check: "check-commit-message failed" in commit_message_validator_test.go
if grep -q 'check-commit-message failed' testcases/commit_message_validator_test.go; then
    echo "PASS: 'check-commit-message failed' message preserved"
else
    echo "FAIL: commit message validator error context lost"
    FAILS=$((FAILS+1))
fi

if [ "$FAILS" -gt 0 ]; then
    echo "FAIL: $FAILS custom message check(s) failed"
    exit 1
fi

echo "PASS: AC6 satisfied"
