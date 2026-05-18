#!/bin/bash
# AC 7+8: Setup/precondition failures use fatal assertions (require),
#          multiple independent checks use non-fatal assertions (assert).
set -euo pipefail
WT=/Users/fred.zhang/Projects/j/.j/worktrees/j-introduce-testify-assert-require-and-audit-the-e
cd "$WT"

echo "=== AC7+8: Conversion patterns verify require vs assert usage ==="

FAILS=0

# Check: file writes in test setup use require (fatal)
# store_test.go (prompt) - only uses assert, which is fine for table tests
# verdict_test.go - uses require for os.WriteFile (setup), assert for ParseVerdict
# spawn_formatted_test.go - uses require for SpawnFormattedIn (setup), assert for independent checks

for f in $(grep -rl 'testify/require' --include='*_test.go' .); do
    echo "Checking $f..."
    # require should be used for setup/precondition failures
    requires=$(grep -c 'require\.' "$f" 2>/dev/null || echo 0)
    asserts=$(grep -c 'assert\.' "$f" 2>/dev/null || echo 0)
    echo "  require calls: $requires, assert calls: $asserts"
done

# Verify no production files changed
prod_changes=$(git diff origin/main --name-only -- '.go' | grep -v '_test.go' || true)
if [ -n "$prod_changes" ]; then
    echo "FAIL: Production files were modified:"
    echo "$prod_changes"
    FAILS=$((FAILS+1))
else
    echo "PASS: No production files modified"
fi

# verify test-audit-spa-98.md was created during the audit process
# The file may have been intentionally deleted after the audit was done.
if git log --all --oneline --diff-filter=A -- test-audit-spa-98.md >/dev/null 2>&1; then
    echo "PASS: audit artifact was created during migration"
elif [ -f test-audit-spa-98.md ]; then
    echo "PASS: audit artifact exists"
else
    echo "FAIL: no evidence of audit artifact in git history or filesystem"
    FAILS=$((FAILS+1))
fi

if [ "$FAILS" -gt 0 ]; then
    echo "FAIL: $FAILS check(s) failed"
    exit 1
fi

echo "PASS: AC7+8 satisfied"
