#!/usr/bin/env bash
# Fail if any of the given staged paths has more than MAX_LINES lines
# in its staged blob. Invoked by lefthook with {staged_files}.
set -euo pipefail

MAX_LINES=300

violations=()
for path in "$@"; do
	lines=$(git show ":$path" | wc -l | tr -d ' ')
	if [ "$lines" -gt "$MAX_LINES" ]; then
		violations+=("$path: $lines lines (limit $MAX_LINES)")
	fi
done

if [ ${#violations[@]} -gt 0 ]; then
	echo "staged non-test file(s) exceed ${MAX_LINES}-line cap:" >&2
	for v in "${violations[@]}"; do
		echo "  - $v" >&2
	done
	echo "split the file or move logic out before committing." >&2
	exit 1
fi
