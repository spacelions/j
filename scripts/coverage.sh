#!/usr/bin/env bash
# coverage.sh: run the Go test suite with per-package coverage for everything
# under internal/... and fail if any non-allowlisted symbol is below 100%
# line coverage.
#
# Note: we intentionally do NOT use -coverpkg=./internal/... here. On Go 1.26
# that flag plus multiple test binaries produces a merged profile that under-
# reports coverage. Per-package coverage is both faster and more accurate for
# gating. Race detection can be run separately via `go test -race ./...`.
set -euo pipefail

cd "$(dirname "$0")/.."

go test -covermode=atomic -coverprofile=cover.out ./internal/...

total=$(go tool cover -func=cover.out | awk '/^total:/ {print $3}')
echo "total coverage: $total"

# Known-uncoverable symbols: error returns from external library calls that
# do not fail with any reasonable inputs we can construct in tests (e.g.
# gemini.NewModel, viper.BindEnv, full.NewLauncher().Execute), or RunE tail
# paths whose happy branch starts the real ADK launcher. Listed as extended
# regex patterns matched against "go tool cover -func" output lines.
allow_patterns=(
  'internal/config/config\.go:[0-9]+:[[:space:]]+Init[[:space:]]'
  'internal/workflow/workflow\.go:[0-9]+:[[:space:]]+Run[[:space:]]'
  'internal/cli/root\.go:[0-9]+:[[:space:]]+Execute[[:space:]]'
)

below=$(go tool cover -func=cover.out | awk '$NF != "100.0%" && !/^total:/ {print}')
for pat in "${allow_patterns[@]}"; do
  below=$(printf '%s\n' "$below" | grep -Ev "$pat" || true)
done
# Strip any blank lines produced by the filters.
below=$(printf '%s\n' "$below" | sed '/^$/d')

if [ -n "$below" ]; then
  echo "the following non-allowlisted symbols are below 100% coverage:" >&2
  echo "$below" >&2
  exit 1
fi
