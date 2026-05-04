#!/usr/bin/env bash
# Point the local repo at scripts/git-hooks/ for its hook directory.
# Idempotent: re-running just rewrites the same value.
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

git config core.hooksPath scripts/git-hooks
echo "core.hooksPath -> $(git config --get core.hooksPath)"
